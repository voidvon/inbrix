package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type aiSkillChangeSourceKey struct{}

// WithAISkillChangeSource carries a trusted transport identity, never a secret.
func WithAISkillChangeSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, aiSkillChangeSourceKey{}, source)
}

type AISkillVersion struct {
	Skill        AISkillRecord `json:"skill"`
	Action       string        `json:"action"`
	Source       string        `json:"source"`
	RestoredFrom int64         `json:"restoredFrom,omitempty"`
	RecordedAt   time.Time     `json:"recordedAt"`
}

func archiveAISkill(ctx context.Context, tx *sql.Tx, skill AISkillRecord, action string, restoredFrom int64) error {
	source, _ := ctx.Value(aiSkillChangeSourceKey{}).(string)
	if source == "" {
		source = "system"
	}
	raw, err := json.Marshal(skill)
	if err != nil {
		return err
	}
	insert := "INSERT"
	if action == "migration" {
		insert = "INSERT OR IGNORE"
	}
	_, err = tx.ExecContext(ctx, insert+` INTO ai_skill_versions(owner_id,id,revision,snapshot_json,action,source,restored_from,created_at) VALUES(?,?,?,?,?,?,?,?)`, skill.OwnerID, skill.ID, skill.Revision, string(raw), action, source, restoredFrom, time.Now().Unix())
	return err
}

// Backfill only the surviving revision of pre-versioning databases. Earlier
// overwritten revisions cannot be reconstructed and are never invented.
func (s *Store) backfillAISkillVersions(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT `+aiSkillColumns+` FROM ai_skills s WHERE NOT EXISTS(SELECT 1 FROM ai_skill_versions v WHERE v.owner_id=s.owner_id AND v.id=s.id AND v.revision=s.revision)`)
	if err != nil {
		return err
	}
	var records []AISkillRecord
	for rows.Next() {
		record, err := scanAISkill(rows)
		if err != nil {
			rows.Close()
			return err
		}
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(records) == 0 {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range records {
		if err := archiveAISkill(ctx, tx, record, "migration", 0); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanAISkillVersion(row interface{ Scan(...any) error }) (AISkillVersion, error) {
	var v AISkillVersion
	var raw string
	var stamp int64
	err := row.Scan(&raw, &v.Action, &v.Source, &v.RestoredFrom, &stamp)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v.Skill)
	}
	v.RecordedAt = timeFromUnix(stamp)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}

func (s *Store) GetAISkillVersion(ctx context.Context, owner, id string, revision int64) (AISkillVersion, error) {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return AISkillVersion{}, err
	}
	v, err := scanAISkillVersion(s.db.QueryRowContext(ctx, `SELECT snapshot_json,action,source,restored_from,created_at FROM ai_skill_versions WHERE owner_id=? AND id=? AND revision=?`, owner, id, revision))
	v.Skill.OwnerID = owner
	return v, err
}

func (s *Store) ListAISkillVersions(ctx context.Context, owner, id string, before int64, limit int) ([]AISkillVersion, error) {
	if err := s.ensureAISkills(ctx, owner); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := s.db.QueryContext(ctx, `SELECT snapshot_json,action,source,restored_from,created_at FROM ai_skill_versions WHERE owner_id=? AND id=? AND revision<? ORDER BY revision DESC LIMIT ?`, owner, id, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []AISkillVersion{}
	for rows.Next() {
		v, err := scanAISkillVersion(rows)
		if err != nil {
			return nil, err
		}
		v.Skill.OwnerID = owner
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// Restoring is a new compare-and-swap revision. Dependency validation applies
// against today's registry; history itself is never modified.
func (s *Store) RestoreAISkillVersion(ctx context.Context, owner, id string, revision, expectedRevision int64) (AISkillRecord, error) {
	if revision < 1 || expectedRevision < 1 {
		return AISkillRecord{}, fmt.Errorf("%w: revision and expected revision must be positive", ErrInvalidAISkill)
	}
	v, err := s.GetAISkillVersion(ctx, owner, id, revision)
	if err != nil {
		return AISkillRecord{}, err
	}
	return s.saveAISkill(ctx, v.Skill, expectedRevision, "restore", revision)
}
