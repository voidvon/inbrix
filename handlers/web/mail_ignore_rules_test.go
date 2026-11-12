package web_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"inbrix/mailstore"
	"inbrix/models"
)

func TestMailIgnoreRulesAPIOwnershipValidationAndNonMutatingPreview(t *testing.T) {
	f := newAPIKeyFixture(t)
	path := "/api/accounts/" + f.account.ID + "/automation-ignore-rules"
	res, body := keyRequest(t, f, "GET", path, f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, "tencent-interception-notice") {
		t.Fatal(res.StatusCode, body)
	}
	input, _ := json.Marshal(map[string]any{"rules": mailstore.DefaultMailIgnoreRules()})
	for _, method := range []string{"GET", "PUT", "POST"} {
		target := path
		if method == "POST" {
			target += "/preview"
		}
		for _, tc := range []struct {
			key    string
			status int
		}{{f.otherKey.Key, 404}, {"", 401}} {
			res, body = keyRequest(t, f, method, target, tc.key, string(input), nil)
			if res.StatusCode != tc.status {
				t.Fatal(method, res.StatusCode, body)
			}
		}
	}
	for _, payload := range []string{`{}`, `{"rules":null}`, `{"rules":{}}`, `{"rules":[{"name":"empty","enabled":true,"conditions":[],"actions":["analysis"]}]}`} {
		res, body = keyRequest(t, f, "PUT", path, f.key.Key, payload, nil)
		if res.StatusCode != 400 {
			t.Fatal("invalid input", payload, res.StatusCode, body)
		}
	}
	if err := f.db.UpsertMessages(context.Background(), f.account.ID, "INBOX", []models.Email{{ID: "1", Subject: "系统邮件拦截通知", MessageID: "<mis_1@exmail.weixin.qq.com>", Body: "PRIVATE-BODY"}, {ID: "2", Subject: "Invoice", MessageID: "<invoice@other.example>"}}); err != nil {
		t.Fatal(err)
	}
	res, body = keyRequest(t, f, "POST", path+"/preview", f.key.Key, string(input), nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"total":1`) || strings.Contains(body, "PRIVATE-BODY") {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "PUT", path, f.key.Key, `{"rules":[]}`, nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"rules":[]`) {
		t.Fatal(res.StatusCode, body)
	}
	// Preview unsaved rules after clearing: it must not persist them.
	res, body = keyRequest(t, f, "POST", path+"/preview", f.key.Key, string(input), nil)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, body)
	}
	res, body = keyRequest(t, f, "GET", path, f.key.Key, "", nil)
	if res.StatusCode != 200 || !strings.Contains(body, `"rules":[]`) {
		t.Fatal("preview saved rules", res.StatusCode, body)
	}
}
