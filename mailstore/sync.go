package mailstore

import (
	"context"
	"fmt"
	"inbrix/config"
	"inbrix/handlers/api"
	"inbrix/models"
	"log"
	"strings"
	"sync"
	"time"
)

// SyncManager owns one long-lived polling loop per persisted mailbox account.
// Each loop uses a separate IMAP connection, so web requests remain free to
// perform writes or on-demand body fetches without sharing a selected mailbox.
type SyncManager struct {
	store    *Store
	key      string
	config   config.MailSyncConfig
	aiClient HTTPClient

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	wake   map[string]chan struct{}
	stop   map[string]chan struct{}
	done   map[string]chan struct{}
}

func NewSyncManager(store *Store, encryptionKey string, cfg config.MailSyncConfig) *SyncManager {
	if cfg.Interval <= 0 {
		cfg.Interval = 60
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	return &SyncManager{
		store:  store,
		key:    encryptionKey,
		config: cfg,
		wake:   make(map[string]chan struct{}),
		stop:   make(map[string]chan struct{}),
		done:   make(map[string]chan struct{}),
	}
}

func (m *SyncManager) Start() error {
	if m == nil || m.store == nil || !m.config.Enabled {
		return nil
	}
	m.mu.Lock()
	if m.cancel == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}
	m.mu.Unlock()
	accounts, err := m.store.ListAllAccounts(context.Background())
	if err != nil {
		return fmt.Errorf("mail sync: list accounts: %w", err)
	}
	for _, account := range accounts {
		m.StartAccount(account.ID)
	}
	return nil
}

// StartAll starts workers for every account. It is intentionally separate from
// Start so callers can supply an account list without exposing an owner-wide
// query to web code.
func (m *SyncManager) StartAll(accounts []Account) {
	if m == nil || !m.config.Enabled {
		return
	}
	m.mu.Lock()
	if m.cancel == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}
	m.mu.Unlock()
	for _, account := range accounts {
		m.StartAccount(account.ID)
	}
}

func (m *SyncManager) StartAccount(accountID string) {
	if m == nil || m.store == nil || accountID == "" || !m.config.Enabled {
		return
	}
	m.mu.Lock()
	if m.cancel == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}
	if _, exists := m.wake[accountID]; exists {
		m.mu.Unlock()
		m.Trigger(accountID)
		return
	}
	wake := make(chan struct{}, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	m.wake[accountID] = wake
	m.stop[accountID] = stop
	m.done[accountID] = done
	ctx := m.ctx
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		defer close(done)
		m.runAccount(ctx, accountID, wake, stop)
	}()
}

// StopAccount stops a mailbox worker after an account is removed. Without
// this, the old worker would keep retrying a deleted account forever.
func (m *SyncManager) StopAccount(accountID string) {
	if m == nil || accountID == "" {
		return
	}
	m.mu.Lock()
	stop := m.stop[accountID]
	done := m.done[accountID]
	delete(m.stop, accountID)
	delete(m.wake, accountID)
	delete(m.done, accountID)
	m.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	if done != nil {
		<-done
	}
}

func (m *SyncManager) Trigger(accountID string) {
	m.mu.Lock()
	wake := m.wake[accountID]
	m.mu.Unlock()
	if wake == nil {
		m.StartAccount(accountID)
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// SyncNewMessagesNow pulls only messages newer than the local high-water mark
// for one mailbox. Compose calls this after APPENDing to Sent so the local
// conversation view is current before the success response reaches the browser.
func (m *SyncManager) SyncNewMessagesNow(ctx context.Context, accountID, folderName string) error {
	if m == nil || m.store == nil || accountID == "" || strings.TrimSpace(folderName) == "" {
		return nil
	}
	account, err := m.store.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if strings.EqualFold(account.AuthType, "oauth2") {
		return fmt.Errorf("mail sync: immediate OAuth2 sync is not available")
	}
	var password string
	if err := api.DecryptJSON(account.EncryptedPassword, &password, m.key); err != nil {
		return fmt.Errorf("decrypt credentials: %w", err)
	}
	client, err := api.NewClientTLS(account.IMAPServer, account.IMAPPort, account.Username, password, account.IMAPTLS)
	if err != nil {
		return fmt.Errorf("connect IMAP: %w", err)
	}
	defer client.Close()

	cursor, err := m.store.MaxMessageUID(ctx, accountID, folderName)
	if err != nil {
		return err
	}
	batch := uint32(m.config.BatchSize)
	if batch == 0 {
		batch = 200
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := client.FetchMessagesSinceUID(folderName, cursor, batch)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		if err := m.store.UpsertMessages(ctx, accountID, folderName, page); err != nil {
			return err
		}
		maxUID := cursor
		for _, email := range page {
			if m.config.SyncBodies {
				if full, fetchErr := client.FetchSingleMessage(folderName, email.ID); fetchErr != nil {
					log.Printf("mail sync: immediate body fetch %s/%s: %v", folderName, email.ID, fetchErr)
				} else if err := m.store.UpsertMessages(ctx, accountID, folderName, []models.Email{full}); err != nil {
					return err
				}
			}
			if uid, parseErr := parseUIDString(email.ID); parseErr == nil && uint32(uid) > maxUID {
				maxUID = uint32(uid)
			}
		}
		if maxUID <= cursor {
			break
		}
		cursor = maxUID
		if len(page) < int(batch) {
			break
		}
	}
	return m.store.UpdateFolderStats(ctx, accountID, folderName)
}

func (m *SyncManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	for accountID, stop := range m.stop {
		delete(m.stop, accountID)
		delete(m.wake, accountID)
		delete(m.done, accountID)
		close(stop)
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *SyncManager) runAccount(ctx context.Context, accountID string, wake, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(m.config.Interval) * time.Second)
	defer ticker.Stop()
	for {
		if err := m.syncAccount(ctx, accountID); err != nil && ctx.Err() == nil {
			log.Printf("mail sync: account %s: %v", accountID, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
		case <-wake:
		}
	}
}

func (m *SyncManager) syncAccount(ctx context.Context, accountID string) error {
	account, err := m.store.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if strings.EqualFold(account.AuthType, "oauth2") {
		return m.store.SetAccountError(ctx, accountID, "background sync for OAuth2 accounts is not available yet")
	}
	var password string
	if err := api.DecryptJSON(account.EncryptedPassword, &password, m.key); err != nil {
		_ = m.store.SetAccountError(ctx, accountID, "decrypt credentials: "+err.Error())
		return err
	}
	client, err := api.NewClientTLS(account.IMAPServer, account.IMAPPort, account.Username, password, account.IMAPTLS)
	if err != nil {
		_ = m.store.SetAccountError(ctx, accountID, "connect IMAP: "+err.Error())
		return err
	}
	defer client.Close()

	folders, err := client.FetchFolders()
	if err != nil {
		_ = m.store.SetAccountError(ctx, accountID, "list folders: "+err.Error())
		return err
	}
	var folderErrors int
	seenFolders := make([]string, 0, len(folders))
	for _, remote := range folders {
		if remote == nil || isNoSelect(remote.Attributes) || strings.TrimSpace(remote.Name) == "" {
			continue
		}
		seenFolders = append(seenFolders, remote.Name)
		folder := Folder{
			AccountID:  accountID,
			Name:       remote.Name,
			Delimiter:  remote.Delimiter,
			Attributes: append([]string(nil), remote.Attributes...),
		}
		if err := m.store.UpsertFolder(ctx, folder); err != nil {
			return err
		}
		if err := m.syncFolder(ctx, client, accountID, remote.Name); err != nil {
			folderErrors++
			log.Printf("mail sync: account %s folder %q: %v", accountID, remote.Name, err)
			_ = m.store.MarkFolderSync(ctx, accountID, remote.Name, false, err)
		}
	}
	if err := m.store.PruneFolders(ctx, accountID, seenFolders); err != nil {
		_ = m.store.SetAccountError(ctx, accountID, "reconcile folders: "+err.Error())
		return err
	}
	if folderErrors > 0 {
		err := fmt.Errorf("%d folder(s) failed to synchronize", folderErrors)
		_ = m.store.SetAccountError(ctx, accountID, err.Error())
		return err
	}
	return m.store.MarkAccountSyncedAt(ctx, accountID, time.Now())
}

func isNoSelect(attributes []string) bool {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute, `\Noselect`) {
			return true
		}
	}
	return false
}

func (m *SyncManager) syncFolder(ctx context.Context, client *api.Client, accountID, folderName string) error {
	state, stateErr := m.store.GetSyncState(ctx, accountID, folderName)
	// A capped initial scan is intentionally marked incomplete, but it still
	// establishes a valid high-water mark. Only a missing state or a recorded
	// fetch error needs another full header scan.
	full := stateErr != nil || state.LastError != ""
	if !full {
		return m.syncFolderIncremental(ctx, client, accountID, folderName)
	}
	limit := m.config.MaxMessagesPerFolder
	batch := m.config.BatchSize
	offset := 0
	complete := true
	var seen []uint32

	for {
		if limit > 0 && offset >= limit {
			complete = false
			break
		}
		page, err := client.FetchMessagesPaged(folderName, uint32(batch), uint32(offset))
		if err != nil {
			return fmt.Errorf("fetch page at offset %d: %w", offset, err)
		}
		if len(page) == 0 {
			break
		}
		if err := m.store.UpsertMessages(ctx, accountID, folderName, page); err != nil {
			return err
		}
		for _, email := range page {
			if uid, err := parseUIDString(email.ID); err == nil {
				seen = append(seen, uint32(uid))
			}
		}
		if err := m.syncBodies(ctx, client, accountID, folderName, page); err != nil {
			return err
		}
		offset += len(page)
		if len(page) < batch {
			break
		}
		if limit > 0 && offset >= limit {
			complete = false
			break
		}
	}

	if full && complete {
		if err := m.store.PruneFolder(ctx, accountID, folderName, seen); err != nil {
			return fmt.Errorf("prune folder: %w", err)
		}
	}
	if err := m.store.UpdateFolderStats(ctx, accountID, folderName); err != nil {
		return err
	}
	return m.store.MarkFolderSync(ctx, accountID, folderName, complete, nil)
}

// syncFolderIncremental refreshes the newest metadata window (so read/unread
// changes appear locally) and then walks every UID after the local high-water
// mark. Walking pages from the oldest unseen UID prevents a burst larger than
// batch_size from being skipped permanently.
func (m *SyncManager) syncFolderIncremental(ctx context.Context, client *api.Client, accountID, folderName string) error {
	batch := uint32(m.config.BatchSize)
	lastUID, err := m.store.MaxMessageUID(ctx, accountID, folderName)
	if err != nil {
		return err
	}

	latest, err := client.FetchMessagesPaged(folderName, batch, 0)
	if err != nil {
		return fmt.Errorf("refresh latest messages: %w", err)
	}
	if len(latest) > 0 {
		if err := m.store.UpsertMessages(ctx, accountID, folderName, latest); err != nil {
			return err
		}
		if err := m.syncBodies(ctx, client, accountID, folderName, latest); err != nil {
			return err
		}
	}

	cursor := lastUID
	var newMessages []models.Email
	for {
		page, err := client.FetchMessagesSinceUID(folderName, cursor, batch)
		if err != nil {
			return fmt.Errorf("fetch new messages after UID %d: %w", cursor, err)
		}
		if len(page) == 0 {
			break
		}
		if err := m.store.UpsertMessages(ctx, accountID, folderName, page); err != nil {
			return err
		}
		if err := m.syncBodies(ctx, client, accountID, folderName, page); err != nil {
			return err
		}
		newMessages = append(newMessages, page...)
		maxUID := cursor
		for _, email := range page {
			if uid, parseErr := parseUIDString(email.ID); parseErr == nil && uint32(uid) > maxUID {
				maxUID = uint32(uid)
			}
		}
		if maxUID <= cursor {
			break
		}
		cursor = maxUID
		if len(page) < int(batch) {
			break
		}
	}

	remoteUIDs, err := client.FetchMessageUIDs(folderName)
	if err != nil {
		return fmt.Errorf("reconcile remote UIDs: %w", err)
	}
	if err := m.store.PruneFolder(ctx, accountID, folderName, remoteUIDs); err != nil {
		return fmt.Errorf("prune deleted messages: %w", err)
	}
	if err := m.store.UpdateFolderStats(ctx, accountID, folderName); err != nil {
		return err
	}
	if err := m.store.MarkFolderSync(ctx, accountID, folderName, true, nil); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(folderName), "INBOX") && len(newMessages) > 0 {
		account, err := m.store.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		m.notifyNewMessages(ctx, client, account, newMessages)
	}
	return nil
}

func (m *SyncManager) syncBodies(ctx context.Context, client *api.Client, accountID, folderName string, page []models.Email) error {
	// The page contains newly discovered messages that may need a full body
	// fetch. The local query adds every older row whose attachment metadata
	// predates the marker, so an incremental sync also repairs the historical
	// mailbox instead of only the newest batch.
	pending := make([]string, 0, len(page))
	pageIDs := make(map[string]struct{}, len(page))
	seen := make(map[string]struct{}, len(page))
	for _, email := range page {
		if email.ID == "" {
			continue
		}
		pending = append(pending, email.ID)
		pageIDs[email.ID] = struct{}{}
		seen[email.ID] = struct{}{}
	}
	missing, err := m.store.ListMessageUIDsMissingAttachmentMetadata(ctx, accountID, folderName)
	if err != nil {
		return err
	}
	for _, uid := range missing {
		if _, ok := seen[uid]; ok {
			continue
		}
		pending = append(pending, uid)
	}

	for _, uid := range pending {
		cached, err := m.store.GetMessage(ctx, accountID, folderName, uid)
		if err != nil {
			return fmt.Errorf("check body cache %s/%s: %w", folderName, uid, err)
		}
		_, isPageMessage := pageIDs[uid]
		if isPageMessage && m.config.SyncBodies && !cached.BodyCached {
			fullMessage, fetchErr := client.FetchSingleMessage(folderName, uid)
			if fetchErr != nil {
				// Header synchronization remains useful when one body is malformed or
				// unavailable, so body failures are logged and do not abort the batch.
				log.Printf("mail sync: fetch body %s/%s: %v", folderName, uid, fetchErr)
			} else {
				if err := m.store.UpsertMessages(ctx, accountID, folderName, []models.Email{fullMessage}); err != nil {
					return err
				}
				continue
			}
		}
		if cached.AttachmentMetadataCached {
			// The body may be intentionally uncached when sync_bodies=false, but
			// there is no metadata work left for this message.
			continue
		}
		attachments, fetchErr := client.FetchAttachmentMetadata(folderName, uid)
		if fetchErr != nil {
			// Header synchronization remains useful when one MIME structure is
			// malformed or unavailable, so metadata failures do not abort the batch.
			log.Printf("mail sync: fetch attachment metadata %s/%s: %v", folderName, uid, fetchErr)
			continue
		}
		if err := m.store.UpdateAttachmentMetadata(ctx, accountID, folderName, uid, attachments); err != nil {
			return err
		}
	}
	return nil
}
