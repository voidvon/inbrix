package mailstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"inbrix/models"
)

// Keep both rare and common terms: an index must help selective searches
// without making the usual first page of common matches prohibitively costly.
func BenchmarkSearchMessages(b *testing.B) {
	ctx := context.Background()
	store, err := Open(filepath.Join(b.TempDir(), "mail.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	user, err := store.CreateUser(ctx, "search-bench", "Search", "password")
	if err != nil {
		b.Fatal(err)
	}
	account, err := store.UpsertAccount(ctx, Account{OwnerID: user.ID, Email: "me@example.com", Username: "me@example.com"})
	if err != nil {
		b.Fatal(err)
	}
	emails := make([]models.Email, 5000)
	for i := range emails {
		body := strings.Repeat("Ordinary project update and delivery details. ", 200)
		if i%1000 == 0 {
			body += " rare-needle 华南项目"
		}
		emails[i] = models.Email{
			ID: fmt.Sprint(i + 1), From: "peer@example.com", To: account.Email,
			Subject: "Project update", Preview: "Delivery details", Body: body,
			HTML: "<p>" + body + "</p>", BodyCached: true,
			Date: time.Unix(int64(i+1), 0),
		}
	}
	if err := store.UpsertMessages(ctx, account.ID, "INBOX", emails); err != nil {
		b.Fatal(err)
	}
	for _, query := range []string{"rare-needle", "absent-needle", "project", "华南项目", "华南"} {
		b.Run(query, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := store.SearchMessages(ctx, account.ID, "INBOX", query, 200); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
