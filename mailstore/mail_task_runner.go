package mailstore

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// Queue policy has no IMAP, credentials, model client, or HTTP-handler knowledge.
// Application execution and sync readiness are supplied by the adapter.
type mailTaskRunner struct {
	store   *Store
	claim   func(context.Context, string, time.Time, string) (MailTask, bool, error)
	execute func(context.Context, Account, MailTask) (bool, error)
	after   func(string) bool
}

func (r *mailTaskRunner) run(ctx context.Context, accountID string, wake, stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	lanes := []string{mailTaskLaneAI, mailTaskLaneNotification}
	wakes := []chan struct{}{make(chan struct{}, 1), make(chan struct{}, 1)}
	for i, lane := range lanes {
		workers.Add(1)
		go func(lane string, signal <-chan struct{}) {
			defer workers.Done()
			r.runLane(ctx, accountID, lane, signal)
		}(lane, wakes[i])
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	defer func() { cancel(); <-done }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-done:
			return
		case <-wake:
			for _, signal := range wakes {
				select {
				case signal <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (r *mailTaskRunner) runLane(ctx context.Context, accountID, lane string, wake <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		account, err := r.store.GetAccount(ctx, accountID)
		if errors.Is(err, ErrNotFound) || ctx.Err() != nil {
			return
		}
		if err == nil {
			err = r.process(ctx, account, 20, lane)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("mail tasks: account %s lane %s: %v", accountID, lane, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

func (r *mailTaskRunner) process(ctx context.Context, account Account, limit int, lane string) error {
	for range limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		task, claimed, err := r.claim(ctx, account.ID, time.Now(), lane)
		if err != nil {
			return err
		}
		if !claimed {
			return nil
		}
		timeout := 100 * time.Second
		if task.Type == mailTaskNotification {
			timeout = 15 * time.Second
		}
		taskCtx, cancel := context.WithTimeout(withMailTaskRun(ctx, task), timeout)
		var skipped bool
		var taskErr error
		if task.Attempts > mailTaskMaxAttempts(task.Type) {
			taskErr = permanentMailTaskError("attempt_limit", errors.New("mail task exceeded its recovery attempt limit"))
		} else {
			skipped, taskErr = r.execute(taskCtx, account, task)
		}
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if taskErr != nil {
			log.Printf("mail task run %s: account %s %s/%s %s attempt %d: %v", task.RunID, account.ID, task.FolderName, task.UID, task.Type, task.Attempts, taskErr)
			err = r.store.failMailTask(cleanupCtx, task, taskErr)
		} else {
			err = r.store.completeMailTask(cleanupCtx, task, skipped)
		}
		cleanupCancel()
		if err != nil {
			return err
		}
		if r.after != nil {
			r.after(account.ID)
		}
	}
	return nil
}
