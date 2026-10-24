package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	AISkillChat        = "chat"
	AISkillMailSummary = "mail_summary"
)

var (
	ErrInvalidAISkill  = errors.New("invalid skill")
	ErrAISkillConflict = errors.New("skill revision conflict")
	ErrAISkillInUse    = errors.New("skill is in use")
	aiSkillIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type AISkillRecord struct {
	ID           string    `json:"id"`
	OwnerID      string    `json:"-"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Instructions string    `json:"instructions"`
	Kind         string    `json:"kind"`
	DependsOn    []string  `json:"dependsOn"`
	Enabled      bool      `json:"enabled"`
	BuiltIn      bool      `json:"builtIn"`
	Revision     int64     `json:"revision"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type AISkillExecution struct {
	ID           string          `json:"id"`
	Revision     int64           `json:"revision"`
	Instructions string          `json:"instructions"`
	Fingerprint  string          `json:"fingerprint"`
	Versions     []AISkillRecord `json:"versions"`
}

const aiSkillColumns = `id,owner_id,name,description,instructions,kind,depends_on_json,enabled,built_in,revision,created_at,updated_at`

func scanAISkill(row interface{ Scan(...any) error }) (AISkillRecord, error) {
	var skill AISkillRecord
	var dependencies string
	var enabled, builtIn int
	var created, updated int64
	err := row.Scan(&skill.ID, &skill.OwnerID, &skill.Name, &skill.Description, &skill.Instructions, &skill.Kind, &dependencies, &enabled, &builtIn, &skill.Revision, &created, &updated)
	if err == nil {
		err = json.Unmarshal([]byte(dependencies), &skill.DependsOn)
	}
	if skill.DependsOn == nil {
		skill.DependsOn = []string{}
	}
	skill.Enabled, skill.BuiltIn = intBool(enabled), intBool(builtIn)
	skill.CreatedAt, skill.UpdatedAt = timeFromUnix(created), timeFromUnix(updated)
	return skill, err
}

func (s *Store) ensureAISkills(ctx context.Context, owner string) error {
	if strings.TrimSpace(owner) == "" {
		return ErrNotFound
	}
	var initialized int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM ai_skill_settings WHERE owner_id=?`, owner).Scan(&initialized); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ai_skill_settings(owner_id,mail_summary_skill_id) VALUES(?,'mail-analysis')`, owner)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		for _, skill := range defaultAISkills() {
			now := time.Now().Unix()
			if _, err := tx.ExecContext(ctx, `INSERT INTO ai_skills(`+aiSkillColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, skill.ID, owner, skill.Name, skill.Description, skill.Instructions, skill.Kind, marshalJSON(skill.DependsOn, "[]"), boolInt(skill.Enabled), 1, 1, now, now); err != nil {
				return err
			}
			skill.OwnerID, skill.BuiltIn, skill.Revision = owner, true, 1
			skill.CreatedAt, skill.UpdatedAt = timeFromUnix(now), timeFromUnix(now)
			if err := archiveAISkill(ctx, tx, skill, "seed", 0); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func listAISkills(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, owner string) ([]AISkillRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+aiSkillColumns+` FROM ai_skills WHERE owner_id=? ORDER BY created_at,id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AISkillRecord, 0)
	for rows.Next() {
		skill, err := scanAISkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, skill)
	}
	return out, rows.Err()
}

func (s *Store) ListAISkills(ctx context.Context, owner string) ([]AISkillRecord, error) {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return nil, err
	}
	return listAISkills(ctx, s.db, owner)
}

func (s *Store) GetAISkill(ctx context.Context, owner, id string) (AISkillRecord, error) {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return AISkillRecord{}, err
	}
	skill, err := scanAISkill(s.db.QueryRowContext(ctx, `SELECT `+aiSkillColumns+` FROM ai_skills WHERE owner_id=? AND id=?`, owner, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AISkillRecord{}, ErrNotFound
	}
	return skill, err
}

func normalizeAISkill(skill AISkillRecord) (AISkillRecord, error) {
	skill.ID, skill.OwnerID, skill.Name, skill.Description, skill.Instructions = strings.TrimSpace(skill.ID), strings.TrimSpace(skill.OwnerID), strings.TrimSpace(skill.Name), strings.TrimSpace(skill.Description), strings.TrimSpace(skill.Instructions)
	if !aiSkillIDPattern.MatchString(skill.ID) || skill.OwnerID == "" || skill.Name == "" || utf8.RuneCountInString(skill.Name) > 100 || utf8.RuneCountInString(skill.Description) > 400 || skill.Instructions == "" || len(skill.Instructions) > 16000 || !utf8.ValidString(skill.Instructions) {
		return skill, fmt.Errorf("%w: 标识只能使用小写字母、数字、下划线和连字符；名称必填且最多100字，说明最多400字，流程必填且最多16000字节", ErrInvalidAISkill)
	}
	if skill.Kind != AISkillChat && skill.Kind != AISkillMailSummary {
		return skill, fmt.Errorf("%w: unsupported skill kind", ErrInvalidAISkill)
	}
	if len(skill.DependsOn) > 12 {
		return skill, fmt.Errorf("%w: 最多依赖12个 Skill", ErrInvalidAISkill)
	}
	seen := map[string]bool{}
	deps := make([]string, 0, len(skill.DependsOn))
	for _, id := range skill.DependsOn {
		id = strings.TrimSpace(id)
		if id == skill.ID || !aiSkillIDPattern.MatchString(id) || seen[id] {
			return skill, fmt.Errorf("%w: 无效或重复的依赖", ErrInvalidAISkill)
		}
		seen[id] = true
		deps = append(deps, id)
	}
	skill.DependsOn = deps
	return skill, nil
}

func resolveAISkillRecords(skills []AISkillRecord, id, kind string) (AISkillExecution, error) {
	byID := map[string]AISkillRecord{}
	for _, skill := range skills {
		byID[skill.ID] = skill
	}
	root, ok := byID[id]
	if !ok {
		return AISkillExecution{}, ErrNotFound
	}
	if root.Kind != kind || !root.Enabled {
		return AISkillExecution{}, fmt.Errorf("%w: Skill 不存在、类型不匹配或已停用", ErrInvalidAISkill)
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var b strings.Builder
	var snapshot []any
	var versions []AISkillRecord
	var add func(string) error
	add = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("%w: Skill 依赖存在循环", ErrInvalidAISkill)
		}
		if visited[id] {
			return nil
		}
		skill, ok := byID[id]
		if !ok || !skill.Enabled || skill.Kind != kind {
			return fmt.Errorf("%w: 依赖 %s 不存在、已停用或类型不匹配", ErrInvalidAISkill, id)
		}
		visiting[id] = true
		for _, dep := range skill.DependsOn {
			if err := add(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		fmt.Fprintf(&b, "\nSkill %s (revision %d):\n%s\n", skill.ID, skill.Revision, skill.Instructions)
		snapshot = append(snapshot, []any{skill.ID, skill.Revision, skill.Instructions})
		versions = append(versions, skill)
		if b.Len() > 24000 {
			return fmt.Errorf("%w: Skill 及其依赖的流程总长度超过24000字节", ErrInvalidAISkill)
		}
		return nil
	}
	if err := add(id); err != nil {
		return AISkillExecution{}, err
	}
	raw, _ := json.Marshal(snapshot)
	return AISkillExecution{ID: id, Revision: root.Revision, Instructions: b.String(), Fingerprint: hashMailSummaryValue(string(raw)), Versions: versions}, nil
}

func validateAISkillGraph(skills []AISkillRecord, activeSummary string) error {
	if len(skills) > 64 {
		return fmt.Errorf("%w: 最多保存64个 Skill", ErrInvalidAISkill)
	}
	byID := map[string]AISkillRecord{}
	for _, skill := range skills {
		byID[skill.ID] = skill
	}
	// Check dependencies even for disabled records, so enabling or deleting one
	// cannot uncover dangling references or cycles later.
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("%w: Skill 依赖存在循环", ErrInvalidAISkill)
		}
		if state[id] == 2 {
			return nil
		}
		skill := byID[id]
		state[id] = 1
		for _, dep := range skill.DependsOn {
			other, ok := byID[dep]
			if !ok || other.Kind != skill.Kind {
				return fmt.Errorf("%w: 依赖 %s 不存在或类型不匹配", ErrAISkillInUse, dep)
			}
			if skill.Enabled && !other.Enabled {
				return fmt.Errorf("%w: 已启用的 Skill %s 依赖 %s，请先修改依赖或停用引用方", ErrAISkillInUse, skill.ID, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for _, skill := range skills {
		if err := visit(skill.ID); err != nil {
			return err
		}
		if skill.Enabled {
			if _, err := resolveAISkillRecords(skills, skill.ID, skill.Kind); err != nil {
				return err
			}
		}
	}
	if root, ok := byID[activeSummary]; !ok || !root.Enabled || root.Kind != AISkillMailSummary {
		return fmt.Errorf("%w: 当前邮件总结 Skill 不能删除或停用，请先切换邮件总结流程", ErrAISkillInUse)
	}
	return nil
}

// SaveAISkill uses revision zero for create and compare-and-swap for update.
// Validation and write share one transaction, including dependency checks.
func (s *Store) SaveAISkill(ctx context.Context, skill AISkillRecord, expectedRevision int64) (AISkillRecord, error) {
	return s.saveAISkill(ctx, skill, expectedRevision, "save", 0)
}

func (s *Store) saveAISkill(ctx context.Context, skill AISkillRecord, expectedRevision int64, action string, restoredFrom int64) (AISkillRecord, error) {
	if skill.ID == "" {
		id, err := newID("skill")
		if err != nil {
			return skill, err
		}
		skill.ID = id
	}
	var err error
	skill, err = normalizeAISkill(skill)
	if err != nil {
		return skill, err
	}
	if err := s.ensureAISkills(ctx, skill.OwnerID); err != nil {
		return skill, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return skill, err
	}
	defer tx.Rollback()
	// Take the SQLite writer reservation before graph inspection.
	if _, err := tx.ExecContext(ctx, `UPDATE ai_skill_settings SET mail_summary_skill_id=mail_summary_skill_id WHERE owner_id=?`, skill.OwnerID); err != nil {
		return skill, err
	}
	skills, err := listAISkills(ctx, tx, skill.OwnerID)
	if err != nil {
		return skill, err
	}
	found := -1
	for i, existing := range skills {
		if existing.ID == skill.ID {
			found = i
			break
		}
	}
	if expectedRevision == 0 {
		if found >= 0 {
			return skill, ErrAISkillConflict
		}
		skill.BuiltIn = false
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM ai_skill_versions WHERE owner_id=? AND id=?`, skill.OwnerID, skill.ID).Scan(&skill.Revision); err != nil {
			return skill, err
		}
		skill.CreatedAt = time.Now().UTC()
		skills = append(skills, skill)
	} else {
		if found < 0 {
			return skill, ErrNotFound
		}
		existing := skills[found]
		if existing.Revision != expectedRevision {
			return skill, ErrAISkillConflict
		}
		if existing.Kind != skill.Kind {
			return skill, fmt.Errorf("%w: Skill 类型创建后不能更改", ErrInvalidAISkill)
		}
		skill.BuiltIn, skill.CreatedAt, skill.Revision = existing.BuiltIn, existing.CreatedAt, existing.Revision+1
		skills[found] = skill
	}
	var active string
	if err := tx.QueryRowContext(ctx, `SELECT mail_summary_skill_id FROM ai_skill_settings WHERE owner_id=?`, skill.OwnerID).Scan(&active); err != nil {
		return skill, err
	}
	if err := validateAISkillGraph(skills, active); err != nil {
		return skill, err
	}
	skill.UpdatedAt = time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_skills(`+aiSkillColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(owner_id,id) DO UPDATE SET name=excluded.name,description=excluded.description,instructions=excluded.instructions,depends_on_json=excluded.depends_on_json,enabled=excluded.enabled,revision=excluded.revision,updated_at=excluded.updated_at`, skill.ID, skill.OwnerID, skill.Name, skill.Description, skill.Instructions, skill.Kind, marshalJSON(skill.DependsOn, "[]"), boolInt(skill.Enabled), boolInt(skill.BuiltIn), skill.Revision, skill.CreatedAt.Unix(), skill.UpdatedAt.Unix())
	if err != nil {
		return skill, err
	}
	if err := archiveAISkill(ctx, tx, skill, action, restoredFrom); err != nil {
		return skill, err
	}
	if err := tx.Commit(); err != nil {
		return skill, err
	}
	return skill, nil
}

func (s *Store) DeleteAISkill(ctx context.Context, owner, id string, expectedRevision int64) error {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE ai_skill_settings SET mail_summary_skill_id=mail_summary_skill_id WHERE owner_id=?`, owner); err != nil {
		return err
	}
	skills, err := listAISkills(ctx, tx, owner)
	if err != nil {
		return err
	}
	found := false
	next := make([]AISkillRecord, 0, len(skills))
	for _, skill := range skills {
		if skill.ID == id {
			found = true
			if expectedRevision > 0 && skill.Revision != expectedRevision {
				return ErrAISkillConflict
			}
		} else {
			next = append(next, skill)
		}
	}
	if !found {
		return ErrNotFound
	}
	var active string
	if err := tx.QueryRowContext(ctx, `SELECT mail_summary_skill_id FROM ai_skill_settings WHERE owner_id=?`, owner).Scan(&active); err != nil {
		return err
	}
	if err := validateAISkillGraph(next, active); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_skills WHERE owner_id=? AND id=?`, owner, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MailSummarySkillID(ctx context.Context, owner string) (string, error) {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return "", err
	}
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT mail_summary_skill_id FROM ai_skill_settings WHERE owner_id=?`, owner).Scan(&id)
	return id, err
}

func (s *Store) SetMailSummarySkill(ctx context.Context, owner, id string, expectedRevision int64) error {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE ai_skill_settings SET mail_summary_skill_id=mail_summary_skill_id WHERE owner_id=?`, owner); err != nil {
		return err
	}
	skills, err := listAISkills(ctx, tx, owner)
	if err != nil {
		return err
	}
	execution, err := resolveAISkillRecords(skills, id, AISkillMailSummary)
	if err != nil {
		return err
	}
	if expectedRevision > 0 && execution.Revision != expectedRevision {
		return ErrAISkillConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_skill_settings SET mail_summary_skill_id=? WHERE owner_id=?`, id, owner); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ResolveMailSummarySkill(ctx context.Context, owner string) (AISkillExecution, error) {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return AISkillExecution{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AISkillExecution{}, err
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT mail_summary_skill_id FROM ai_skill_settings WHERE owner_id=?`, owner).Scan(&id); err != nil {
		return AISkillExecution{}, err
	}
	skills, err := listAISkills(ctx, tx, owner)
	if err != nil {
		return AISkillExecution{}, err
	}
	return resolveAISkillRecords(skills, id, AISkillMailSummary)
}
