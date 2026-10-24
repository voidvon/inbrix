package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type MailTaskSkillVersion struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
type MailTaskRunContext struct {
	ModelID          string                 `json:"modelId,omitempty"`
	ModelName        string                 `json:"modelName,omitempty"`
	Provider         string                 `json:"provider,omitempty"`
	ConfigHash       string                 `json:"configHash,omitempty"`
	SkillFingerprint string                 `json:"skillFingerprint,omitempty"`
	SkillVersions    []MailTaskSkillVersion `json:"skillVersions,omitempty"`
	Cached           bool                   `json:"cached"`
	Stale            bool                   `json:"stale"`
}

type MailTaskRun struct {
	ID         string             `json:"id"`
	Folder     string             `json:"folder"`
	MessageID  string             `json:"messageId"`
	TaskType   string             `json:"taskType"`
	Attempt    int                `json:"attempt"`
	Status     string             `json:"status"`
	Phase      string             `json:"phase"`
	StartedAt  time.Time          `json:"startedAt"`
	FinishedAt time.Time          `json:"finishedAt"`
	DurationMS int64              `json:"durationMs"`
	ErrorKind  string             `json:"errorKind,omitempty"`
	Error      string             `json:"error,omitempty"`
	Retryable  bool               `json:"retryable"`
	Context    MailTaskRunContext `json:"context"`
}

type mailTaskRunKey struct{}

func withMailTaskRun(ctx context.Context, task MailTask) context.Context {
	return context.WithValue(ctx, mailTaskRunKey{}, task)
}

func (s *Store) recordMailTaskRunContext(ctx context.Context, model AIModelRecord, configHash string, skill AISkillExecution) error {
	task, ok := ctx.Value(mailTaskRunKey{}).(MailTask)
	if !ok {
		return nil
	}
	metadata := MailTaskRunContext{ModelID: model.ID, ModelName: model.Model, Provider: model.Provider, ConfigHash: configHash, SkillFingerprint: skill.Fingerprint}
	for _, version := range skill.Versions {
		metadata.SkillVersions = append(metadata.SkillVersions, MailTaskSkillVersion{ID: version.ID, Revision: version.Revision})
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mail_task_runs SET context_json=?,phase='generation' WHERE lease_token=? AND status='running'`, string(raw), task.Token)
	return checkMailTaskUpdate(result, err)
}

func (s *Store) recordMailTaskPhase(ctx context.Context, phase string) error {
	task, ok := ctx.Value(mailTaskRunKey{}).(MailTask)
	if !ok {
		return nil
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mail_task_runs SET phase=? WHERE lease_token=? AND status='running'`, phase, task.Token)
	return checkMailTaskUpdate(result, err)
}

func (s *Store) recordMailTaskCacheResult(ctx context.Context, cached, stale bool, record MessageSummaryRecord) error {
	task, ok := ctx.Value(mailTaskRunKey{}).(MailTask)
	if !ok {
		return nil
	}
	if cached {
		metadata := MailTaskRunContext{ModelID: record.ModelID, ModelName: record.ModelName, ConfigHash: record.ConfigHash, Cached: true, Stale: stale}
		if record.Analysis != nil {
			metadata.SkillFingerprint = record.Analysis.SkillSnapshot.Fingerprint
			for _, version := range record.Analysis.SkillSnapshot.Versions {
				metadata.SkillVersions = append(metadata.SkillVersions, MailTaskSkillVersion{ID: version.ID, Revision: version.Revision})
			}
		}
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		result, err := s.db.ExecContext(ctx, `UPDATE mail_task_runs SET context_json=?,phase='result' WHERE lease_token=? AND status='running'`, string(raw), task.Token)
		return checkMailTaskUpdate(result, err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mail_task_runs SET context_json=json_set(context_json,'$.cached',json(?),'$.stale',json(?)),phase='result' WHERE lease_token=? AND status='running'`, fmt.Sprint(cached), fmt.Sprint(stale), task.Token)
	return checkMailTaskUpdate(result, err)
}

func startMailTaskRun(ctx context.Context, tx *sql.Tx, task *MailTask, now time.Time) error {
	id, err := newID("mail_run")
	if err != nil {
		return err
	}
	task.RunID = id
	stamp := now.UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE mail_task_runs SET status='expired',finished_at=?,duration_ms=MAX(0,?-started_at),error_kind='lease_expired',error_message='Worker lease expired before completion',retryable=1 WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND status='running'`, stamp, stamp, task.AccountID, task.FolderName, task.UID, task.Type); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mail_task_runs(id,lease_token,account_id,folder_name,uid,task_type,attempt,status,phase,started_at) VALUES(?,?,?,?,?,?,?,'running','configuration',?)`, id, task.Token, task.AccountID, task.FolderName, task.UID, task.Type, task.Attempts, stamp); err != nil {
		return err
	}
	// Bound journal growth, including repeated explicit retries. The current
	// attempt is always retained even when timestamps tie or clocks move back.
	_, err = tx.ExecContext(ctx, `DELETE FROM mail_task_runs WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND id<>? AND id NOT IN(SELECT id FROM mail_task_runs WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND id<>? ORDER BY started_at DESC,rowid DESC LIMIT 49)`, task.AccountID, task.FolderName, task.UID, task.Type, id, task.AccountID, task.FolderName, task.UID, task.Type, id)
	return err
}

func finishMailTaskRun(ctx context.Context, tx *sql.Tx, task MailTask, status, kind, message string, retryable bool) error {
	stamp := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `UPDATE mail_task_runs SET status=?,finished_at=?,duration_ms=MAX(0,?-started_at),error_kind=?,error_message=?,retryable=? WHERE lease_token=? AND status='running'`, status, stamp, stamp, kind, message, boolInt(retryable), task.Token)
	return err
}

func (s *Store) ListMailTaskRuns(ctx context.Context, accountID, folder, uid, kind string, limit int) ([]MailTaskRun, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	query := `SELECT id,folder_name,uid,task_type,attempt,status,phase,started_at,finished_at,duration_ms,error_kind,error_message,retryable,context_json FROM mail_task_runs WHERE account_id=?`
	args := []any{accountID}
	if folder != "" {
		query += ` AND folder_name=?`
		args = append(args, folder)
	}
	if uid != "" {
		n, err := parseUIDString(uid)
		if err != nil {
			return nil, err
		}
		query += ` AND uid=?`
		args = append(args, n)
	}
	if kind != "" {
		query += ` AND task_type=?`
		args = append(args, kind)
	}
	query += ` ORDER BY started_at DESC,rowid DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []MailTaskRun{}
	for rows.Next() {
		var run MailTaskRun
		var uid, started, finished int64
		var retryable int
		var raw string
		if err := rows.Scan(&run.ID, &run.Folder, &uid, &run.TaskType, &run.Attempt, &run.Status, &run.Phase, &started, &finished, &run.DurationMS, &run.ErrorKind, &run.Error, &retryable, &raw); err != nil {
			return nil, err
		}
		run.MessageID = fmt.Sprint(uid)
		run.StartedAt = time.UnixMilli(started).UTC()
		if finished > 0 {
			run.FinishedAt = time.UnixMilli(finished).UTC()
		}
		run.Retryable = retryable != 0
		if err := json.Unmarshal([]byte(raw), &run.Context); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
