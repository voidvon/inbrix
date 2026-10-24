package mailstore

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"inbrix/internal/agentruntime"
	"inbrix/models"
)

//go:embed skills/mail-analysis/SKILL.md
var mailAnalysisSkill string

type MailAnalysisSource struct {
	Folder      string              `json:"folder"`
	UID         string              `json:"uid"`
	MessageID   string              `json:"messageId,omitempty"`
	ContentHash string              `json:"contentHash"`
	Ranges      []MailAnalysisRange `json:"ranges"`
	TotalLength int                 `json:"totalLength"`
}

type MailAnalysisRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type MailAnalysisActivity struct {
	Name    string         `json:"name"`
	Args    map[string]any `json:"args"`
	Status  string         `json:"status"`
	Summary string         `json:"summary"`
}

// MailAnalysis is durable task output, separate from the short configured
// display fields. Sources and execution activity are assigned by the server.
type MailAnalysis struct {
	Retrieval     MailAnalysisRetrieval  `json:"retrieval"`
	SkillSnapshot AISkillExecution       `json:"skillSnapshot"`
	Request       string                 `json:"request"`
	Background    string                 `json:"background"`
	Changes       string                 `json:"changes"`
	Actions       []string               `json:"actions"`
	Missing       []string               `json:"missing"`
	Fields        map[string]string      `json:"fields"`
	Sources       []MailAnalysisSource   `json:"sources"`
	Activity      []MailAnalysisActivity `json:"activity"`
	Skill         string                 `json:"skill"`
	AsOf          time.Time              `json:"asOf"`
}

type mailAnalysisScope struct {
	retrieval    MailAnalysisRetrieval
	reader       AgentMailReader
	thread       []MessageKey
	conversation []MessageKey
	previews     []map[string]any
	limited      bool
	sources      []MailAnalysisSource
	pending      map[string]models.Email
}

func mailEvidenceHash(message models.Email) string {
	// Include facts exposed by both initial input and read_email, not flags or
	// preview-cache bookkeeping that changes when the user reads a message.
	raw, _ := json.Marshal([]any{message.From, message.To, message.Cc, message.Subject, unixOrZero(message.Date), message.MessageID, message.InReplyTo, message.References, message.Body, message.HTML, message.BodyCached, message.Attachments})
	return hashMailSummaryValue(string(raw))
}

func (scope *mailAnalysisScope) addSource(message models.Email, start, end, total int) error {
	for i, source := range scope.sources {
		if source.Folder == message.Folder && source.UID == message.ID {
			if source.ContentHash != mailEvidenceHash(message) {
				return errors.New("mail evidence changed during analysis; retry with the current content")
			}
			scope.sources[i].Ranges = append(scope.sources[i].Ranges, MailAnalysisRange{Start: start, End: end})
			return nil
		}
	}
	scope.sources = append(scope.sources, MailAnalysisSource{Folder: message.Folder, UID: message.ID, MessageID: message.MessageID, ContentHash: mailEvidenceHash(message), Ranges: []MailAnalysisRange{{Start: start, End: end}}, TotalLength: total})
	return nil
}

func sourceFullyRead(source MailAnalysisSource) bool {
	ranges := append([]MailAnalysisRange{}, source.Ranges...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	end := 0
	for _, span := range ranges {
		if span.Start > end {
			return false
		}
		end = max(end, span.End)
	}
	return end >= source.TotalLength
}

func mailPeers(message models.Email, ownAddress string) string {
	peers := map[string]bool{}
	for _, value := range []string{message.From, message.To} {
		addresses, err := mail.ParseAddressList(value)
		if err != nil {
			continue
		}
		for _, address := range addresses {
			value := strings.ToLower(address.Address)
			if !strings.EqualFold(value, ownAddress) {
				peers[value] = true
			}
		}
	}
	values := make([]string, 0, len(peers))
	for value := range peers {
		values = append(values, value)
	}
	sort.Strings(values)
	return strings.Join(values, "\x00")
}

// Resolve bounded headers only. Explicit RFC references are loaded separately,
// so an old direct parent cannot be displaced by recent unrelated customer mail.
func newMailAnalysisScope(ctx context.Context, store *Store, account Account, message models.Email) (*mailAnalysisScope, error) {
	scope := &mailAnalysisScope{retrieval: MailAnalysisRetrieval{Version: 1, Queries: []MailAnalysisQuery{}}, reader: AgentMailReader{Store: store, AccountID: account.ID, AsOf: message.Date, Anchor: MessageKey{FolderName: message.Folder, UID: message.ID}}, thread: []MessageKey{}, conversation: []MessageKey{}, pending: map[string]models.Email{}}
	if account.ID == "" {
		return scope, nil
	}
	refs := append([]string{directParentMessageID(message), message.MessageID}, message.References...)
	if len(refs) > 100 {
		refs = refs[:100]
	}
	refSet := map[string]bool{}
	var filters []string
	var args []any
	for _, ref := range refs {
		if ref == "" || refSet[ref] {
			continue
		}
		refSet[ref] = true
		filters = append(filters, "message_id = ?")
		args = append(args, ref)
	}
	peers := mailPeers(message, account.Email)
	for _, peer := range strings.Split(peers, "\x00") {
		if peer == "" {
			continue
		}
		filters = append(filters, "(instr(lower(from_addr), ?) > 0 OR instr(lower(to_addrs), ?) > 0)")
		args = append(args, peer, peer)
	}
	if len(filters) == 0 {
		return scope, nil
	}
	base, baseArgs := "account_id = ?", []any{account.ID}
	if !message.Date.IsZero() {
		base += " AND date_unix <= ?"
		baseArgs = append(baseArgs, message.Date.Unix())
	}
	if uid, err := parseUIDString(message.ID); err == nil && message.Folder != "" {
		base += " AND (folder_name <> ? OR uid <= ?)"
		baseArgs = append(baseArgs, message.Folder, uid)
	}
	var err error
	scope.retrieval.ScopeHash, err = mailAnalysisHeaderFingerprint(ctx, store, []string{base, "(" + strings.Join(filters, " OR ") + ")"}, append(append([]any{}, baseArgs...), args...))
	if err != nil {
		return nil, err
	}
	load := func(filters []string, args []any, limit int) ([]models.Email, error) {
		parameters := append(append([]any{}, baseArgs...), args...)
		parameters = append(parameters, limit)
		rows, err := store.db.QueryContext(ctx, "SELECT "+messageHeaderColumns+" FROM messages WHERE "+base+" AND ("+strings.Join(filters, " OR ")+") ORDER BY date_unix DESC, folder_name, uid DESC LIMIT ?", parameters...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var messages []models.Email
		for rows.Next() {
			email, err := scanMessageHeader(rows)
			if err != nil {
				return nil, err
			}
			messages = append(messages, email)
		}
		return messages, rows.Err()
	}
	headers, err := load(filters, args, 201)
	if err != nil {
		return nil, err
	}
	if len(headers) > 200 {
		scope.limited = true
		headers = headers[:200]
	}
	if len(refSet) > 0 {
		var directFilters []string
		var directArgs []any
		for ref := range refSet {
			directFilters = append(directFilters, "message_id = ?")
			directArgs = append(directArgs, ref)
		}
		direct, err := load(directFilters, directArgs, 100)
		if err != nil {
			return nil, err
		}
		headers = append(direct, headers...)
	}
	seen := map[string]bool{}
	for _, email := range headers {
		key := MessageKey{FolderName: email.Folder, UID: email.ID}
		id := MessageSummaryLookupKey(email.Folder, email.ID)
		if seen[id] {
			continue
		}
		seen[id] = true
		sameThread := refSet[email.MessageID] || refSet[email.InReplyTo]
		for _, ref := range email.References {
			sameThread = sameThread || refSet[ref]
		}
		samePeers := peers != "" && mailPeers(email, account.Email) == peers
		if sameThread {
			scope.thread = append(scope.thread, key)
		}
		if sameThread || samePeers {
			scope.conversation = append(scope.conversation, key)
			if len(scope.previews) < 8 {
				scope.previews = append(scope.previews, analysisMailPreview(email))
			}
		}
	}
	return scope, nil
}

func analysisMailPreview(message models.Email) map[string]any {
	return map[string]any{"folder": message.Folder, "uid": message.ID, "messageId": message.MessageID, "inReplyTo": message.InReplyTo, "from": clipAnalysisText(message.From, 300), "to": clipAnalysisText(message.To, 500), "subject": clipAnalysisText(message.Subject, 500), "date": message.Date, "snippet": clipAnalysisText(message.Preview, 600)}
}

func clipAnalysisText(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	for !utf8.RuneStart(text[maxBytes]) {
		maxBytes--
	}
	return text[:maxBytes]
}

func analysisStringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := args[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func analysisIntArg(args map[string]any, key string, fallback, maximum int) (int, error) {
	if value, exists := args[key]; exists {
		n, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil || n < 0 || n > maximum {
			return 0, fmt.Errorf("%s must be between 0 and %d", key, maximum)
		}
		return n, nil
	}
	return fallback, nil
}

func (scope *mailAnalysisScope) execute(ctx context.Context, call agentruntime.ToolCall) (any, string, error) {
	switch call.Name {
	case "search_emails":
		requested := analysisStringArg(call.Args, "scope")
		keys := scope.thread
		switch requested {
		case "", "thread":
			requested = "thread"
		case "conversation":
			keys = scope.conversation
		case "mailbox":
			keys = nil
		default:
			return nil, "", errors.New("scope must be thread, conversation or mailbox")
		}
		limit, err := analysisIntArg(call.Args, "limit", 5, 10)
		if err != nil || limit == 0 {
			return nil, "", errors.New("limit must be between 1 and 10")
		}
		query, cursor := analysisStringArg(call.Args, "query", "q"), analysisStringArg(call.Args, "cursor")
		if len(query) > 2000 || len(cursor) > 2048 {
			return nil, "", errors.New("query or cursor is too long")
		}
		if err := scope.recordQuery(ctx, requested, query); err != nil {
			return nil, "", err
		}
		page, err := scope.reader.Search(ctx, requested, query, cursor, limit, keys)
		if err != nil {
			return nil, "", err
		}
		previews := make([]map[string]any, 0, len(page.Messages))
		for _, email := range page.Messages {
			preview := analysisMailPreview(email)
			preview["snippet"] = clipAnalysisText(AgentMailText(email), 600)
			previews = append(previews, preview)
		}
		return map[string]any{"scope": requested, "messages": previews, "hasMore": page.HasMore, "nextCursor": page.NextCursor, "scopeLimited": requested != "mailbox" && scope.limited, "notice": "Previews locate evidence; read_email is required to verify historical facts. Only synchronized mail is searchable."}, fmt.Sprintf("检索到 %d 封邮件", len(previews)), nil
	case "read_email":
		folder, uid := analysisStringArg(call.Args, "folder"), analysisStringArg(call.Args, "uid", "id", "message_id", "email_id")
		if folder == "" || uid == "" {
			return nil, "", errors.New("folder and uid are required")
		}
		offset, err := analysisIntArg(call.Args, "offset", 0, 100_000_000)
		if err != nil {
			return nil, "", err
		}
		length, err := analysisIntArg(call.Args, "length", 2400, 4000)
		if err != nil || length == 0 {
			return nil, "", errors.New("length must be between 1 and 4000")
		}
		email, err := scope.reader.Read(ctx, folder, uid)
		if err != nil {
			return nil, "", err
		}
		body := []rune(AgentMailText(email))
		if offset > len(body) {
			return nil, "", errors.New("offset exceeds message length")
		}
		end := min(offset+length, len(body))
		available := email.BodyCached || email.Body != "" || email.HTML != ""
		result := analysisMailPreview(email)
		delete(result, "snippet")
		result["body"], result["offset"], result["totalLength"], result["bodyAvailable"], result["hasMore"], result["nextOffset"] = string(body[offset:end]), offset, len(body), available, end < len(body), end
		result["cc"], result["attachments"] = clipAnalysisText(email.Cc, 1000), email.Attachments
		if available {
			scope.pending[call.ID] = email
		}
		return result, fmt.Sprintf("读取 %s/%s（%d–%d 字）", folder, uid, offset, end), nil
	default:
		return nil, "", fmt.Errorf("tool %q is not allowed by the mail-analysis skill", call.Name)
	}
}

func runMailAnalysis(ctx context.Context, client HTTPClient, store *Store, account Account, message models.Email, cfg mailSummaryConfig) (*MailAnalysis, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	scope, err := newMailAnalysisScope(ctx, store, account, message)
	if err != nil {
		return nil, err
	}
	content := partitionMailAnalysisContent(message)
	fullLength := utf8.RuneCountInString(content.FullText)
	content.FullText = clipAnalysisText(content.FullText, 16000)
	truncated := utf8.RuneCountInString(content.FullText) < fullLength
	content.NewText, content.Quoted, content.Signature = clipAnalysisText(content.NewText, 4000), clipAnalysisText(content.Quoted, 4000), clipAnalysisText(content.Signature, 1500)
	current := analysisMailPreview(message)
	current["content"], current["bodyAvailable"], current["attachments"] = content, message.BodyCached || message.Body != "" || message.HTML != "", message.Attachments
	current["bodyTruncated"], current["nextOffset"], current["totalLength"] = truncated, utf8.RuneCountInString(content.FullText), fullLength
	initial, _ := json.Marshal(map[string]any{"account": account.Email, "asOf": message.Date, "triggeringMail": current, "relatedMailPreviews": scope.previews, "scopeLimited": scope.limited, "threadCount": len(scope.thread), "conversationCount": len(scope.conversation)})
	if message.BodyCached || message.Body != "" || message.HTML != "" {
		if err := scope.addSource(message, 0, utf8.RuneCountInString(content.FullText), fullLength); err != nil {
			return nil, err
		}
	}
	instructions := mailSummaryInstructions(cfg.agent, cfg.skill.Instructions)
	run, err := agentruntime.Run(ctx, agentruntime.Options{
		Input:  string(initial),
		Limits: agentruntime.Limits{Turns: 6, CallsPerTurn: 4, Calls: 12, InputBytes: 96000 - len(instructions), ResultBytes: 18000},
		Respond: func(ctx context.Context, input string) (string, error) {
			return CreateAIResponse(ctx, client, cfg.model, cfg.apiKey, instructions, input, 6000, cfg.effort)
		},
		Execute: scope.execute,
		OnTool: func(kind string, call agentruntime.ToolCall) error {
			if kind != "tool_end" || call.Status != "success" || call.Name != "read_email" {
				return nil
			}
			result, ok := call.Result.(map[string]any)
			if !ok || result["outputOmitted"] == true {
				return nil
			}
			if email, ok := scope.pending[call.ID]; ok {
				start, _ := result["offset"].(int)
				end, _ := result["nextOffset"].(int)
				total, _ := result["totalLength"].(int)
				if err := scope.addSource(email, start, end, total); err != nil {
					return err
				}
				delete(scope.pending, call.ID)
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Request    string            `json:"request"`
		Background string            `json:"background"`
		Changes    string            `json:"changes"`
		Actions    []string          `json:"actions"`
		Missing    []string          `json:"missing"`
		Fields     map[string]string `json:"fields"`
	}
	decoder := json.NewDecoder(strings.NewReader(CleanJSONFence(run.Text)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.Fields == nil || result.Actions == nil || result.Missing == nil || strings.TrimSpace(result.Request) == "" || len(result.Fields) != len(cfg.agent.OutputLabels) {
		return nil, fmt.Errorf("%w: mail-analysis skill returned invalid structured output", ErrInvalidMailAnalysis)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: mail-analysis skill returned trailing output", ErrInvalidMailAnalysis)
	}
	var structure map[string]json.RawMessage
	if json.Unmarshal([]byte(CleanJSONFence(run.Text)), &structure) != nil || len(structure) != 6 {
		return nil, fmt.Errorf("%w: mail-analysis skill returned incomplete structured output", ErrInvalidMailAnalysis)
	}
	fields, _ := json.Marshal(result.Fields)
	if _, err := formatTaggedMailSummary(string(fields), cfg.agent.OutputLabels); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMailAnalysis, err)
	}
	skillVersion := "mail-analysis/v1"
	if cfg.skill.ID != "" {
		skillVersion = fmt.Sprintf("%s/v%d", cfg.skill.ID, cfg.skill.Revision)
	}
	analysis := &MailAnalysis{Request: result.Request, Background: result.Background, Changes: result.Changes, Actions: result.Actions, Missing: result.Missing, Fields: result.Fields, Sources: scope.sources, Skill: skillVersion, SkillSnapshot: cfg.skill, Retrieval: scope.retrieval, AsOf: message.Date}
	for _, source := range scope.sources {
		if !sourceFullyRead(source) {
			if source.Folder == message.Folder && source.UID == message.ID {
				return nil, errors.New("mail-analysis skill did not finish reading the triggering email")
			}
			analysis.Missing = append(analysis.Missing, fmt.Sprintf("历史邮件 %s/%s 仅部分读取。", source.Folder, source.UID))
		}
	}
	for _, call := range run.Calls {
		analysis.Activity = append(analysis.Activity, MailAnalysisActivity{Name: call.Name, Args: call.Args, Status: call.Status, Summary: clipAnalysisText(call.Summary, 1000)})
		if call.Status == "error" {
			analysis.Missing = append(analysis.Missing, call.Summary)
		}
		if result, ok := call.Result.(map[string]any); ok {
			if result["outputOmitted"] == true {
				analysis.Missing = append(analysis.Missing, "部分工具结果超出预算，未提供给模型。")
			}
			if call.Name == "read_email" && result["bodyAvailable"] == false {
				analysis.Missing = append(analysis.Missing, "部分历史邮件正文尚未同步。")
			}
		}
	}
	if message.Date.IsZero() {
		analysis.Missing = append(analysis.Missing, "触发邮件时间未知，历史时间范围无法严格确认。")
	}
	for _, attachment := range message.Attachments {
		if !attachment.IsInline {
			analysis.Missing = append(analysis.Missing, "本次仅提供附件信息，未读取附件内容。")
			break
		}
	}
	if err := validateMailAnalysisReferences(analysis); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMailAnalysis, err)
	}
	if account.ID != "" {
		current, err := currentMailAnalysisRetrieval(ctx, store, account, message, analysis.Retrieval)
		if err != nil {
			return nil, err
		}
		before, _ := json.Marshal(analysis.Retrieval)
		after, _ := json.Marshal(current)
		if string(before) != string(after) {
			return nil, errors.New("mail retrieval scope changed during analysis; retry")
		}
		for _, source := range analysis.Sources {
			if source.Folder == message.Folder && source.UID == message.ID {
				continue
			}
			mail, err := scope.reader.Read(ctx, source.Folder, source.UID)
			if err != nil || mailEvidenceHash(mail) != source.ContentHash {
				return nil, errors.New("mail evidence changed during analysis; retry")
			}
		}
	}
	return analysis, nil
}

var mailAnalysisReferenceRE = regexp.MustCompile(`\[([^\]\n]+)/([0-9]+)\]`)

func validateMailAnalysisReferences(analysis *MailAnalysis) error {
	known := map[string]bool{}
	for _, source := range analysis.Sources {
		known[source.Folder+"/"+source.UID] = true
	}
	if strings.TrimSpace(analysis.Background) != "" && !mailAnalysisReferenceRE.MatchString(analysis.Background) {
		return errors.New("mail-analysis background is missing email source references")
	}
	texts := append([]string{analysis.Request, analysis.Background, analysis.Changes}, analysis.Actions...)
	texts = append(texts, analysis.Missing...)
	for _, text := range texts {
		for _, ref := range mailAnalysisReferenceRE.FindAllStringSubmatch(text, -1) {
			if !known[ref[1]+"/"+ref[2]] {
				return errors.New("mail-analysis cited an email that was not read")
			}
		}
	}
	return nil
}
