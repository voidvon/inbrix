package mailstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	mailapi "lilmail/handlers/api"
	"lilmail/models"
)

const (
	mailSummaryPipelineVersion = 2
	mailSummaryGenerationLease = 2 * time.Minute
)

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
	configValue := fmt.Sprintf("v%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", mailSummaryPipelineVersion, model.ID, model.Model, effort, agent.ID, agent.Prompt, inquiryOutputRules)
	return hashMailSummaryValue(configValue)
}

func hashMailSummaryValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func MailSummarySourceHash(account Account, message models.Email) string {
	return hashMailSummaryValue(fullEmailForInquiryAnalysis(account, message))
}

func CurrentMailSummaryConfigHash(ctx context.Context, store *Store, ownerID string) (string, error) {
	agent, model, effort, err := resolveMailSummaryMetadata(ctx, store, ownerID)
	if err != nil {
		return "", err
	}
	return mailSummaryConfigHash(agent, model, effort), nil
}

func resolveMailSummaryMetadata(ctx context.Context, store *Store, ownerID string) (AIAgentRecord, AIModelRecord, string, error) {
	agent, err := store.GetMailSummaryAgent(ctx, ownerID)
	if err != nil {
		return AIAgentRecord{}, AIModelRecord{}, "", fmt.Errorf("load mail summary agent: %w", err)
	}
	model, err := store.GetDefaultAIModel(ctx, ownerID)
	if err != nil {
		return AIAgentRecord{}, AIModelRecord{}, "", fmt.Errorf("load default AI model: %w", err)
	}
	effort := strings.TrimSpace(model.ReasoningEffort)
	if effort == "" {
		effort = "medium"
	}
	return agent, model, effort, nil

}

func resolveMailSummaryConfig(ctx context.Context, store *Store, encryptionKey, ownerID string) (mailSummaryConfig, error) {
	agent, model, effort, err := resolveMailSummaryMetadata(ctx, store, ownerID)
	if err != nil {
		return mailSummaryConfig{}, err
	}
	var apiKey string
	if model.EncryptedAPIKey == "" || mailapi.DecryptJSON(model.EncryptedAPIKey, &apiKey, encryptionKey) != nil || strings.TrimSpace(apiKey) == "" {
		return mailSummaryConfig{}, errors.New("decrypt OpenAI API key")
	}
	return mailSummaryConfig{agent: agent, model: model, apiKey: apiKey, effort: effort, configHash: mailSummaryConfigHash(agent, model, effort)}, nil
}

func generateMailSummary(ctx context.Context, client HTTPClient, account Account, message models.Email, cfg mailSummaryConfig) (string, error) {
	raw, err := createOpenAIWebhookResponse(ctx, client, cfg.model, cfg.apiKey, cfg.agent.Prompt+"\n\n"+inquiryOutputRules, fullEmailForInquiryAnalysis(account, message), cfg.effort)
	if err != nil {
		return "", err
	}
	summary, err := formatMailSummary(raw)
	if err != nil {
		return "", err
	}
	if !isSimplifiedChineseDominant(summary) {
		return "", errors.New("OpenAI analysis did not satisfy simplified Chinese requirements")
	}
	return summary, nil
}

type mailSummaryFields struct {
	Company      string `json:"company"`
	Country      string `json:"country"`
	Products     string `json:"products"`
	Requirements string `json:"requirements"`
	Question     string `json:"question"`
}

func formatMailSummary(raw string) (string, error) {
	var fields mailSummaryFields
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &fields); err != nil {
		return "", errors.New("OpenAI analysis did not return the required summary template")
	}
	company := compactMailSummaryField(fields.Company, 40)
	country := compactMailSummaryField(fields.Country, 24)
	customer := strings.Join(nonEmptyStrings(company, country), "、")
	if customer == "" {
		customer = "未提及"
	}
	products := compactMailSummaryField(fields.Products, 80)
	if products == "" {
		products = "未提及"
	}
	requirements := compactMailSummaryField(fields.Requirements, 80)
	if requirements == "" {
		requirements = "未提及具体要求"
	}
	lines := []string{
		"客户：" + customer,
		"需求：" + products,
		"要求：" + requirements,
	}
	if question := compactMailSummaryField(fields.Question, 80); question != "" {
		lines = append(lines, "问题："+question)
	}
	return strings.Join(lines, "\n"), nil
}

func compactMailSummaryField(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

// SummarizeMail analyzes one complete email. It is independent from delivery
// channels so background notifications and interactive clients share exactly
// the same prompt, model selection, and output validation.
func SummarizeMail(ctx context.Context, client HTTPClient, store *Store, encryptionKey string, account Account, message models.Email) (string, error) {
	cfg, err := resolveMailSummaryConfig(ctx, store, encryptionKey, account.OwnerID)
	if err != nil {
		return "", err
	}
	return generateMailSummary(ctx, client, account, message, cfg)
}

// GetOrCreateMailSummary returns a durable summary and only calls the model
// when no ready result exists. Regeneration is always an explicit caller
// choice; model or prompt changes merely mark an existing result stale.
func GetOrCreateMailSummary(ctx context.Context, client HTTPClient, store *Store, encryptionKey string, account Account, message models.Email, regenerate bool) (MailSummaryResult, error) {
	key := MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}
	sourceHash := MailSummarySourceHash(account, message)
	if existing, err := store.GetMessageSummary(ctx, key); err == nil && existing.Status == "ready" && !regenerate {
		stale := existing.SourceHash != sourceHash
		if configHash, cfgErr := CurrentMailSummaryConfigHash(ctx, store, account.OwnerID); cfgErr == nil {
			stale = stale || existing.ConfigHash != configHash
		}
		return MailSummaryResult{Record: existing, Cached: true, Stale: stale}, nil
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return MailSummaryResult{}, err
	}

	cfg, err := resolveMailSummaryConfig(ctx, store, encryptionKey, account.OwnerID)
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

	summary, generationErr := generateMailSummary(ctx, client, account, message, cfg)
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
