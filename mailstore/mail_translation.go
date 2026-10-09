package mailstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mailapi "inbrix/handlers/api"
	"inbrix/models"
)

const (
	mailTranslationPipelineVersion = 1
	mailTranslationGenerationLease = 2 * time.Minute
)

const mailTranslationSystemPrompt = `你是一个专业的商务多语言邮件翻译引擎。
请将输入邮件的正文内容进行专业、准确、通顺的翻译。
翻译规则：
1. 若原文主要为外语（如英语、日语、俄语、德语等），请翻译为简体中文；若原文主要为中文，请翻译为地道流利的英文。
2. 极其重要：只翻译当前邮件的正文内容本身，绝对不要翻译任何历史引用内容、原邮件引用（如 "On ... wrote:"、">" 引用等）、往来邮件链或邮件头元信息。
3. 严格保留原文的段落排版格式，不要添加多余的解释、客套话或前言后语。
4. 直接输出翻译后的正文文本。`

type MailTranslationResult struct {
	Record MessageSummaryRecord
	Cached bool
}

type mailTranslationConfig struct {
	agent      AIAgentRecord
	model      AIModelRecord
	apiKey     string
	effort     string
	configHash string
}

func resolveMailTranslationConfig(ctx context.Context, store *Store, encryptionKey string, account Account) (mailTranslationConfig, error) {
	var agent AIAgentRecord
	var model AIModelRecord
	binding, err := store.GetAITaskBinding(ctx, account.OwnerID, account.ID, MailTranslationTask)
	if err == nil {
		if !binding.Enabled {
			return mailTranslationConfig{}, ErrNotFound
		}
		agent, err = store.GetAIAgent(ctx, account.OwnerID, binding.AgentID)
		if err == nil {
			model, err = store.GetAIModel(ctx, account.OwnerID, binding.ModelID)
		}
	} else if errors.Is(err, ErrNotFound) {
		model, err = store.GetDefaultAIModel(ctx, account.OwnerID)
	}
	if err != nil {
		return mailTranslationConfig{}, fmt.Errorf("load translation configuration: %w", err)
	}
	var apiKey string
	if model.EncryptedAPIKey == "" || mailapi.DecryptJSON(model.EncryptedAPIKey, &apiKey, encryptionKey) != nil || strings.TrimSpace(apiKey) == "" {
		return mailTranslationConfig{}, errors.New("decrypt AI model API key")
	}
	effort := strings.TrimSpace(model.ReasoningEffort)
	if effort == "" {
		effort = "low"
	}
	instructions := mailTranslationSystemPrompt
	if strings.TrimSpace(agent.Prompt) != "" {
		instructions += "\n\nAgent instructions:\n" + strings.TrimSpace(agent.Prompt)
	}
	configHash := hashMailSummaryValue(fmt.Sprintf("v%d\x00%s\x00%s\x00%s\x00%s\x00%s", mailTranslationPipelineVersion, model.ID, model.Model, effort, agent.ID, instructions))
	return mailTranslationConfig{agent: agent, model: model, apiKey: apiKey, effort: effort, configHash: configHash}, nil
}

func mailTranslationInput(message models.Email) string {
	return strings.TrimSpace(currentMessageText(message.Body, message.HTML))
}

func GetOrCreateMailTranslation(ctx context.Context, client HTTPClient, store *Store, encryptionKey string, account Account, message models.Email, regenerate bool) (MailTranslationResult, error) {
	inputText := mailTranslationInput(message)
	if inputText == "" {
		return MailTranslationResult{}, errors.New("邮件正文为空，无需翻译")
	}

	key := MessageSummaryKey{
		AccountID:  account.ID,
		FolderName: message.Folder,
		UID:        message.ID,
	}

	sourceHash := hashMailSummaryValue(inputText)
	if !regenerate {
		if existing, err := store.GetMessageTranslation(ctx, key); err == nil && existing.Status == "ready" && strings.TrimSpace(existing.Summary) != "" {
			return MailTranslationResult{Record: existing, Cached: true}, nil
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return MailTranslationResult{}, err
		}
	}

	cfg, err := resolveMailTranslationConfig(ctx, store, encryptionKey, account)
	if err != nil {
		_ = store.RecordAIError(ctx, AIErrorLogRecord{
			OwnerID:      account.OwnerID,
			TaskType:     MailTranslationTask,
			AccountEmail: account.Email,
			ErrorMessage: err.Error(),
		})
		return MailTranslationResult{}, err
	}

	claim := MessageSummaryRecord{
		MessageSummaryKey: key,
		SourceHash:        sourceHash,
		ConfigHash:        cfg.configHash,
		ModelID:           cfg.model.ID,
		ModelName:         cfg.model.Model,
		AgentID:           cfg.agent.ID,
		PipelineVersion:   mailTranslationPipelineVersion,
	}

	current, claimed, err := store.ClaimMessageTranslationGeneration(ctx, claim, regenerate, mailTranslationGenerationLease)
	if err != nil {
		_ = store.RecordAIError(ctx, AIErrorLogRecord{
			OwnerID:      account.OwnerID,
			TaskType:     MailTranslationTask,
			AccountEmail: account.Email,
			ModelName:    cfg.model.Model,
			AgentName:    cfg.agent.Name,
			ErrorMessage: err.Error(),
		})
		return MailTranslationResult{}, err
	}

	if !claimed {
		if current.Status == "ready" && !regenerate {
			return MailTranslationResult{Record: current, Cached: true}, nil
		}
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = store.RecordAIError(ctx, AIErrorLogRecord{
					OwnerID:      account.OwnerID,
					TaskType:     MailTranslationTask,
					AccountEmail: account.Email,
					ModelName:    cfg.model.Model,
					AgentName:    cfg.agent.Name,
					ErrorMessage: ctx.Err().Error(),
				})
				return MailTranslationResult{}, ctx.Err()
			case <-ticker.C:
				current, err = store.GetMessageTranslation(ctx, key)
				if err != nil {
					return MailTranslationResult{}, err
				}
				if current.Status == "ready" {
					return MailTranslationResult{Record: current, Cached: true}, nil
				}
				if current.Status == "failed" || !current.LeaseUntil.After(time.Now()) {
					return GetOrCreateMailTranslation(ctx, client, store, encryptionKey, account, message, regenerate)
				}
			}
		}
	}

	instructions := mailTranslationSystemPrompt
	if strings.TrimSpace(cfg.agent.Prompt) != "" {
		instructions += "\n\nAgent instructions:\n" + strings.TrimSpace(cfg.agent.Prompt)
	}

	translation, generationErr := createOpenAIWebhookResponse(ctx, client, cfg.model, cfg.apiKey, instructions, inputText, cfg.effort)
	if generationErr != nil {
		_ = store.FailMessageTranslationGeneration(ctx, key, current.GenerationToken, generationErr)
		_ = store.RecordAIError(ctx, AIErrorLogRecord{
			OwnerID:      account.OwnerID,
			TaskType:     MailTranslationTask,
			AccountEmail: account.Email,
			ModelName:    cfg.model.Model,
			AgentName:    cfg.agent.Name,
			ErrorMessage: generationErr.Error(),
		})
		return MailTranslationResult{}, generationErr
	}

	completed, err := store.CompleteMessageTranslationGeneration(ctx, claim, current.GenerationToken, strings.TrimSpace(translation))
	if err != nil {
		return MailTranslationResult{}, err
	}
	return MailTranslationResult{Record: completed}, nil
}
