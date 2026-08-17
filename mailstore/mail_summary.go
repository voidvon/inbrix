package mailstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	mailapi "lilmail/handlers/api"
	"lilmail/models"
)

const (
	mailSummaryPipelineVersion = 6
	mailSummaryGenerationLease = 2 * time.Minute
)

var mailSummaryMessageIDRe = regexp.MustCompile(`<[^>]+>`)

const mailSummarySystemPrompt = `你是一个邮件分析引擎。请只分析输入中实际存在的信息，不得编造。结果必须精简，每个字段只写一行且不超过 80 个字；缺失的信息使用空字符串。严格遵守下方智能体提示词和输出格式。`

// HTTPClient is the minimal client contract needed by mail summarization and
// optional delivery channels.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type MailSummaryResult struct {
	Record MessageSummaryRecord
	Cached bool
	Stale  bool
}

type mailSummaryConfig struct {
	agent      AIAgentRecord
	model      AIModelRecord
	apiKey     string
	effort     string
	configHash string
}

func mailSummaryConfigHash(agent AIAgentRecord, model AIModelRecord, effort string) string {
	labelsJSON, _ := json.Marshal(agent.OutputLabels)
	configValue := fmt.Sprintf("v%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", mailSummaryPipelineVersion, model.ID, model.Model, effort, agent.ID, agent.Prompt, labelsJSON, mailSummarySystemPrompt)
	return hashMailSummaryValue(configValue)
}

func hashMailSummaryValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func MailSummarySourceHash(account Account, message models.Email) string {
	return hashMailSummaryValue(mailSummaryInput(account, message, ""))
}

// CurrentMailSummarySourceHash includes the ready summary of the direct parent,
// when available, because changing that summary changes the composed result.
func CurrentMailSummarySourceHash(ctx context.Context, store *Store, account Account, message models.Email) (string, error) {
	previous, err := previousMailSummary(ctx, store, account.ID, message)
	if err != nil {
		return "", err
	}
	return hashMailSummaryValue(mailSummaryInput(account, message, previous)), nil
}

func CurrentMailSummaryConfigHash(ctx context.Context, store *Store, account Account) (string, error) {
	agent, model, effort, err := resolveMailSummaryMetadata(ctx, store, account)
	if err != nil {
		return "", err
	}
	return mailSummaryConfigHash(agent, model, effort), nil
}

func resolveMailSummaryMetadata(ctx context.Context, store *Store, account Account) (AIAgentRecord, AIModelRecord, string, error) {
	var agent AIAgentRecord
	var model AIModelRecord
	binding, err := store.GetAITaskBinding(ctx, account.OwnerID, account.ID, MailSummaryTask)
	if err == nil {
		agent, err = store.GetAIAgent(ctx, account.OwnerID, binding.AgentID)
		if err == nil {
			model, err = store.GetAIModel(ctx, account.OwnerID, binding.ModelID)
		}
	} else if errors.Is(err, ErrNotFound) {
		agent, err = store.GetMailSummaryAgent(ctx, account.OwnerID)
		if err == nil {
			model, err = store.GetDefaultAIModel(ctx, account.OwnerID)
		}
	}
	if err != nil {
		return AIAgentRecord{}, AIModelRecord{}, "", fmt.Errorf("load mail summary configuration: %w", err)
	}
	effort := strings.TrimSpace(model.ReasoningEffort)
	if effort == "" {
		effort = "medium"
	}
	return agent, model, effort, nil

}

func resolveMailSummaryConfig(ctx context.Context, store *Store, encryptionKey string, account Account) (mailSummaryConfig, error) {
	agent, model, effort, err := resolveMailSummaryMetadata(ctx, store, account)
	if err != nil {
		return mailSummaryConfig{}, err
	}
	var apiKey string
	if model.EncryptedAPIKey == "" || mailapi.DecryptJSON(model.EncryptedAPIKey, &apiKey, encryptionKey) != nil || strings.TrimSpace(apiKey) == "" {
		return mailSummaryConfig{}, errors.New("decrypt OpenAI API key")
	}
	return mailSummaryConfig{agent: agent, model: model, apiKey: apiKey, effort: effort, configHash: mailSummaryConfigHash(agent, model, effort)}, nil
}

func generateMailSummary(ctx context.Context, client HTTPClient, account Account, message models.Email, previous string, cfg mailSummaryConfig) (string, error) {
	raw, err := createOpenAIWebhookResponse(ctx, client, cfg.model, cfg.apiKey, mailSummaryInstructions(cfg.agent), mailSummaryInput(account, message, previous), cfg.effort)
	if err != nil {
		return "", err
	}
	return formatTaggedMailSummary(raw, cfg.agent.OutputLabels)
}

func mailSummaryInstructions(agent AIAgentRecord) string {
	labelsJSON, _ := json.Marshal(agent.OutputLabels)
	return mailSummarySystemPrompt + "\n\n智能体提示词：\n" + agent.Prompt + "\n\n输出格式：\n只输出一个合法 JSON 对象，不要输出 Markdown、代码块或解释。JSON 必须且只能包含这些键，并保持字段值为字符串：" + string(labelsJSON)
}

func formatTaggedMailSummary(raw string, labels []string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &fields); err != nil {
		return "", errors.New("OpenAI analysis did not return the configured output fields")
	}
	allowed := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		allowed[label] = struct{}{}
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return "", errors.New("OpenAI analysis returned an unconfigured output field")
		}
	}
	lines := make([]string, 0, len(labels))
	for _, label := range labels {
		var value string
		if field, ok := fields[label]; ok {
			if err := json.Unmarshal(field, &value); err != nil {
				return "", errors.New("OpenAI analysis returned a non-string output field")
			}
		}
		value = compactMailSummaryValue(value, 80)
		if value != "" {
			lines = append(lines, label+"："+value)
		}
	}
	if len(lines) == 0 {
		return "", errors.New("OpenAI analysis returned no configured output values")
	}
	return strings.Join(lines, "\n"), nil
}

func compactMailSummaryValue(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

func mailSummaryInput(account Account, message models.Email, previous string) string {
	current := fullEmailForInquiryAnalysis(account, message)
	previous = strings.TrimSpace(previous)
	if previous == "" {
		return current
	}
	return "上一封邮件的已保存分析（仅供上下文参考）：\n" + previous + "\n\n当前邮件：\n" + current
}

func previousMailSummary(ctx context.Context, store *Store, accountID string, message models.Email) (string, error) {
	parentID := directParentMessageID(message)
	if parentID == "" {
		return "", nil
	}
	record, err := store.GetReadyMessageSummaryByMessageID(ctx, accountID, parentID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(record.Summary), nil
}

func directParentMessageID(message models.Email) string {
	parentID := strings.TrimSpace(message.InReplyTo)
	if parentID == "" && len(message.References) > 0 {
		parentID = strings.TrimSpace(message.References[len(message.References)-1])
	}
	if matches := mailSummaryMessageIDRe.FindAllString(parentID, -1); len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return parentID
}

// SummarizeMail analyzes one complete email. It is independent from delivery
// channels so background notifications and interactive clients share exactly
// the same configured prompt and model selection.
func SummarizeMail(ctx context.Context, client HTTPClient, store *Store, encryptionKey string, account Account, message models.Email) (string, error) {
	cfg, err := resolveMailSummaryConfig(ctx, store, encryptionKey, account)
	if err != nil {
		return "", err
	}
	previous, err := previousMailSummary(ctx, store, account.ID, message)
	if err != nil {
		return "", err
	}
	return generateMailSummary(ctx, client, account, message, previous, cfg)
}

// GetOrCreateMailSummary returns a durable summary and only calls the model
// when no ready result exists. Regeneration is always an explicit caller
// choice; model or prompt changes merely mark an existing result stale.
func GetOrCreateMailSummary(ctx context.Context, client HTTPClient, store *Store, encryptionKey string, account Account, message models.Email, regenerate bool) (MailSummaryResult, error) {
	key := MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}
	previous, err := previousMailSummary(ctx, store, account.ID, message)
	if err != nil {
		return MailSummaryResult{}, err
	}
	sourceHash := hashMailSummaryValue(mailSummaryInput(account, message, previous))
	if existing, err := store.GetMessageSummary(ctx, key); err == nil && existing.Status == "ready" && !regenerate {
		stale := existing.SourceHash != sourceHash
		if configHash, cfgErr := CurrentMailSummaryConfigHash(ctx, store, account); cfgErr == nil {
			stale = stale || existing.ConfigHash != configHash
		}
		return MailSummaryResult{Record: existing, Cached: true, Stale: stale}, nil
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return MailSummaryResult{}, err
	}

	cfg, err := resolveMailSummaryConfig(ctx, store, encryptionKey, account)
	if err != nil {
		return MailSummaryResult{}, err
	}
	claim := MessageSummaryRecord{
		MessageSummaryKey: key,
		SourceHash:        sourceHash,
		ConfigHash:        cfg.configHash,
		ModelID:           cfg.model.ID,
		ModelName:         cfg.model.Model,
		AgentID:           cfg.agent.ID,
		PipelineVersion:   mailSummaryPipelineVersion,
	}
	current, claimed, err := store.ClaimMessageSummaryGeneration(ctx, claim, regenerate, mailSummaryGenerationLease)
	if err != nil {
		return MailSummaryResult{}, err
	}
	if !claimed {
		if current.Status == "ready" {
			return MailSummaryResult{Record: current, Cached: true, Stale: current.SourceHash != sourceHash || current.ConfigHash != cfg.configHash}, nil
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return MailSummaryResult{}, ctx.Err()
			case <-ticker.C:
				current, err = store.GetMessageSummary(ctx, key)
				if err != nil {
					return MailSummaryResult{}, err
				}
				if current.Status == "ready" {
					return MailSummaryResult{Record: current, Cached: true, Stale: current.SourceHash != sourceHash || current.ConfigHash != cfg.configHash}, nil
				}
				if current.Status == "failed" {
					return MailSummaryResult{}, errors.New(current.ErrorMessage)
				}
				if !current.LeaseUntil.After(time.Now()) {
					return GetOrCreateMailSummary(ctx, client, store, encryptionKey, account, message, regenerate)
				}
			}
		}
	}

	summary, generationErr := generateMailSummary(ctx, client, account, message, previous, cfg)
	if generationErr != nil {
		_ = store.FailMessageSummaryGeneration(ctx, key, current.GenerationToken, generationErr)
		return MailSummaryResult{}, generationErr
	}
	completed, err := store.CompleteMessageSummaryGeneration(ctx, claim, current.GenerationToken, summary)
	if err != nil {
		return MailSummaryResult{}, err
	}
	return MailSummaryResult{Record: completed, Cached: false, Stale: false}, nil
}
