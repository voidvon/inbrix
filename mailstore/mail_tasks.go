package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	mailTaskAnalysis         = "analysis"
	mailTaskReply            = "reply"
	mailTaskNotification     = "notification"
	mailTaskLease            = 3 * time.Minute
	mailTaskNotificationWait = 30 * time.Second
	mailTaskMaxConcurrentAI  = 4
	mailTaskLaneAI           = "ai"
	mailTaskLaneNotification = "notification"
)

type MailTask struct {
	MessageSummaryKey
	Type         string
	Status       string
	Attempts     int
	Token        string
	RunID        string
	LeaseUntil   time.Time
	NextAttempt  time.Time
	Payload      string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const mailTaskColumns = `account_id, folder_name, uid, task_type, status, attempts, generation_token, lease_until, next_attempt, payload, error_message, created_at, updated_at`

func scanMailTask(scanner interface{ Scan(...any) error }) (MailTask, error) {
	var task MailTask
	var uid, lease, next, created, updated int64
	err := scanner.Scan(&task.AccountID, &task.FolderName, &uid, &task.Type, &task.Status, &task.Attempts, &task.Token, &lease, &next, &task.Payload, &task.ErrorMessage, &created, &updated)
	task.UID = fmt.Sprint(uid)
	task.LeaseUntil = timeFromUnix(lease)
	task.NextAttempt = timeFromUnix(next)
	task.CreatedAt, task.UpdatedAt = timeFromUnix(created), timeFromUnix(updated)
	return task, err
}

// Called inside the same transaction as the new-message insert. A crash cannot
// advance the IMAP high-water mark without leaving recoverable work behind.
func enqueueMailTasksTx(ctx context.Context, tx *sql.Tx, accountID, folder string, uid, now int64) error {
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mail_tasks WHERE account_id=? AND folder_name=? AND uid=?`, accountID, folder, uid).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mail_tasks(account_id,folder_name,uid,task_type,status,attempts,generation_token,lease_until,next_attempt,payload,error_message,created_at,updated_at)
		SELECT ?,?,?,task_type,'pending',0,'',0,?,'','',?,? FROM
		(SELECT 'analysis' AS task_type UNION ALL SELECT 'notification' UNION ALL SELECT 'reply')
		WHERE task_type <> 'notification' OR EXISTS(SELECT 1 FROM account_webhook_settings WHERE account_id=? AND enabled=1 AND url<>'')`, accountID, folder, uid, now, now, now, accountID)
	return err
}

func (s *Store) enqueueMailTasks(ctx context.Context, accountID, folder string, uids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, raw := range uids {
		uid, err := parseUIDString(raw)
		if err != nil {
			return err
		}
		if err := enqueueMailTasksTx(ctx, tx, accountID, folder, uid, time.Now().Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClaimMailTask drains all task types serially for maintenance callers. Live
// workers use independent AI and notification lanes via claimMailTaskInLane.
func (s *Store) ClaimMailTask(ctx context.Context, accountID string, now time.Time) (MailTask, bool, error) {
	return s.claimMailTaskInLane(ctx, accountID, now, "")
}

func (s *Store) claimMailTaskInLane(ctx context.Context, accountID string, now time.Time, lane string) (MailTask, bool, error) {
	token, err := newID("mail_task")
	if err != nil {
		return MailTask{}, false, err
	}
	candidate, active, order := "1", "1", "q.uid,CASE q.task_type WHEN 'analysis' THEN 0 WHEN 'notification' THEN 1 ELSE 2 END,q.folder_name"
	if lane == mailTaskLaneAI {
		candidate = "q.task_type IN ('analysis','reply')"
		active = "active.task_type IN ('analysis','reply')"
		order = "CASE q.task_type WHEN 'analysis' THEN 0 ELSE 1 END,q.created_at,q.folder_name,q.uid"
	}
	if lane == mailTaskLaneNotification {
		candidate = "q.task_type='notification'"
		active = "active.task_type='notification'"
		order = "q.created_at,q.folder_name,q.uid"
	}
	if lane != "" && lane != mailTaskLaneAI && lane != mailTaskLaneNotification {
		return MailTask{}, false, errors.New("invalid mail task lane")
	}
	args := []any{token, now.Add(mailTaskLease).Unix(), now.Unix(), accountID, now.Unix(), now.Unix(), now.Unix(), now.Unix(), mailTaskMaxConcurrentAI}
	eligibility := ""
	if lane == mailTaskLaneNotification {
		eligibility = ` AND (q.payload<>'' OR q.created_at<=? OR EXISTS(SELECT 1 FROM message_summaries summary WHERE summary.account_id=q.account_id AND summary.folder_name=q.folder_name AND summary.uid=q.uid AND summary.summary_type='mail_summary' AND summary.status='ready' AND trim(summary.summary_text)<>'') OR NOT EXISTS(SELECT 1 FROM mail_tasks analysis WHERE analysis.account_id=q.account_id AND analysis.folder_name=q.folder_name AND analysis.uid=q.uid AND analysis.task_type='analysis' AND (analysis.status='running' OR (analysis.status='pending' AND analysis.attempts=0))))`
		args = append(args, now.Add(-mailTaskNotificationWait).Unix())
	}
	query := `UPDATE mail_tasks SET status='running', attempts=attempts+1, generation_token=?, lease_until=?, updated_at=?
		WHERE (account_id,folder_name,uid,task_type)=(
		 SELECT q.account_id,q.folder_name,q.uid,q.task_type FROM mail_tasks q
		 WHERE q.account_id=? AND ((q.status='pending' AND q.next_attempt<=?) OR (q.status='running' AND q.lease_until<=?))
		 AND ` + candidate + `
		 AND NOT EXISTS(SELECT 1 FROM mail_tasks active WHERE active.account_id=q.account_id AND active.status='running' AND active.lease_until>? AND ` + active + `)
		 AND (q.task_type='notification' OR (SELECT COUNT(*) FROM mail_tasks busy WHERE busy.task_type IN ('analysis','reply') AND busy.status='running' AND busy.lease_until>?)<?)` + eligibility + `
		 ORDER BY ` + order + ` LIMIT 1)
		RETURNING ` + mailTaskColumns
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MailTask{}, false, err
	}
	defer tx.Rollback()
	task, err := scanMailTask(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return MailTask{}, false, nil
	}
	if err != nil {
		return task, false, err
	}
	if err := startMailTaskRun(ctx, tx, &task, now); err != nil {
		return task, false, err
	}
	if err := tx.Commit(); err != nil {
		return task, false, err
	}
	return task, true, nil
}

func (s *Store) GetMailTask(ctx context.Context, key MessageSummaryKey, kind string) (MailTask, error) {
	_, uid, err := normalizeMessageSummaryKey(key)
	if err != nil {
		return MailTask{}, err
	}
	task, err := scanMailTask(s.db.QueryRowContext(ctx, `SELECT `+mailTaskColumns+` FROM mail_tasks WHERE account_id=? AND folder_name=? AND uid=? AND task_type=?`, key.AccountID, key.FolderName, uid, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return MailTask{}, ErrNotFound
	}
	return task, err
}

func (s *Store) completeMailTask(ctx context.Context, task MailTask, skipped bool) error {
	status := "ready"
	if skipped {
		status = "skipped"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE mail_tasks SET status=?,generation_token='',lease_until=0,next_attempt=0,error_message='',updated_at=? WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND generation_token=? AND status='running'`, status, time.Now().Unix(), task.AccountID, task.FolderName, task.UID, task.Type, task.Token)
	if err := checkMailTaskUpdate(result, err); err != nil {
		return err
	}
	if err := finishMailTaskRun(ctx, tx, task, status, "", "", false); err != nil {
		return err
	}
	return tx.Commit()
}

func mailTaskMaxAttempts(kind string) int {
	switch kind {
	case mailTaskAnalysis:
		return 5
	case mailTaskNotification:
		return 8
	default:
		return 3
	}
}

func mailTaskRetryDelay(attempts int) time.Duration {
	delay := 15 * time.Second
	for i := 1; i < attempts && delay < 30*time.Minute; i++ {
		delay *= 2
	}
	return min(delay, 30*time.Minute)
}

func (s *Store) failMailTask(ctx context.Context, task MailTask, cause error) error {
	policy := classifyMailTaskFailure(task, cause)
	status, runStatus, attempts := "pending", "failed", task.Attempts
	next := time.Now().Add(max(mailTaskRetryDelayWithJitter(task), policy.delay)).Unix()
	if !policy.retryable {
		status, next = "failed", 0
	}
	if policy.interrupted {
		attempts = max(0, attempts-1)
		next = time.Now().Unix()
		runStatus = "interrupted"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE mail_tasks SET status=?,attempts=?,generation_token='',lease_until=0,next_attempt=?,error_message=?,updated_at=? WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND generation_token=? AND status='running'`, status, attempts, next, cause.Error(), time.Now().Unix(), task.AccountID, task.FolderName, task.UID, task.Type, task.Token)
	if err := checkMailTaskUpdate(result, err); err != nil {
		return err
	}
	if err := finishMailTaskRun(ctx, tx, task, runStatus, policy.kind, cause.Error(), policy.retryable); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) saveMailTaskPayload(ctx context.Context, task MailTask, payload string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mail_tasks SET payload=? WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND generation_token=? AND status='running'`, payload, task.AccountID, task.FolderName, task.UID, task.Type, task.Token)
	return checkMailTaskUpdate(result, err)
}

func checkMailTaskUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("mail task lease was lost")
	}
	return nil
}

func (s *Store) ListMailTasks(ctx context.Context, accountID, folder, uid string, limit int) ([]MailTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `SELECT ` + mailTaskColumns + ` FROM mail_tasks WHERE account_id=?`
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
	query += ` ORDER BY updated_at DESC,uid DESC,task_type LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]MailTask, 0)
	for rows.Next() {
		task, err := scanMailTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// Explicit retries reset terminal failures, including configuration failures.
// Successful deliveries cannot
// be accidentally replayed through this operation.
func (s *Store) RetryMailTask(ctx context.Context, key MessageSummaryKey, kind string) error {
	_, uid, err := normalizeMessageSummaryKey(key)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mail_tasks SET status='pending',attempts=0,next_attempt=?,error_message='',updated_at=? WHERE account_id=? AND folder_name=? AND uid=? AND task_type=? AND status='failed'`, time.Now().Unix(), time.Now().Unix(), key.AccountID, key.FolderName, uid, kind)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}
