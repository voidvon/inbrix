package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/mailstore"
)

func TestDocumentCatalogReferenceChecksUseCurrentOwner(t *testing.T) {
	h, _, account, other := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
	ctx := context.Background()
	local := businessTestProduct(t, h, account.OwnerID, "A")
	foreign := businessTestProduct(t, h, other.OwnerID, "B")
	input := aiDocumentInput{Instruction: local.ID + " " + local.ID + " prod_external", History: []documentChatMessage{{Role: "user", Content: foreign.ID}}}
	checks, err := h.documentCatalogReferenceContext(ctx, account.OwnerID, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":"` + local.ID + `","available":true`, `"id":"` + foreign.ID + `","available":false`, `"id":"prod_external","available":false`, "Manual lines are allowed"} {
		if !strings.Contains(checks, want) {
			t.Fatal("missing owner-scoped reference check", want, checks)
		}
	}
	if strings.Count(checks, local.ID) != 1 {
		t.Fatal("reference checks duplicated", checks)
	}
	restricted := context.WithValue(ctx, businessRuntimeKey{}, businessRuntime{Brands: []string{"A"}})
	checks, err = h.documentCatalogReferenceContext(restricted, account.OwnerID, input)
	if err != nil || !strings.Contains(checks, "Manual lines are not allowed") {
		t.Fatal(checks, err)
	}
}

func TestDocumentAssistantValidatesCatalogAndRenderedQuotationBeforeAccepting(t *testing.T) {
	for _, mode := range []string{"correct manual quotation", "unavailable product remains", "restricted mailbox"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h, app, account, _ := agentContextTestApp(t, func(request agentTestRequest) string {
				calls++
				if !strings.Contains(request.Input, `"id":"prod_external_dp27","available":false`) {
					t.Error("missing current catalog validation evidence")
				}
				if calls == 2 && (!strings.Contains(request.Input, "validationError") || !strings.Contains(request.Input, "当前账号产品库")) {
					t.Error("missing failed reference feedback")
				}
				reply := documentEditReply{Commands: []DocumentCommand{}, Message: "按用户提供的美元价格手工报价；外部目录编号未绑定"}
				if mode == "restricted mailbox" && calls == 2 {
					reply.Message = "当前品牌范围要求关联本地产品，请选择可用的本地产品或调整邮箱品牌范围。"
				} else {
					fields := map[string]json.RawMessage{"currency": json.RawMessage(`"USD"`)}
					reply.Commands = append(reply.Commands, DocumentCommand{Type: "set_fields", Fields: fields})
					for i, model := range []string{"DP27 DN40", "SV607 DN40/DN65"} {
						price := []string{"3714.00", "1350.00"}[i]
						encoded, _ := json.Marshal(map[string]string{"model": model, "qty": "1", "price": price})
						var line map[string]json.RawMessage
						_ = json.Unmarshal(encoded, &line)
						if calls == 1 || mode == "unavailable product remains" {
							line["productId"] = json.RawMessage(`"prod_external_dp27"`)
							line["productVersion"] = json.RawMessage(`9`)
						}
						reply.Commands = append(reply.Commands, DocumentCommand{Type: "add_line", Fields: line})
					}
				}
				encoded, _ := json.Marshal(reply)
				return string(encoded)
			})
			ctx := context.Background()
			if mode == "restricted mailbox" {
				if err := h.mailDB.SaveAccountBrands(ctx, account.OwnerID, account.ID, mailstore.AccountBrands{BrandMode: "restrict", Brands: []string{"A"}}); err != nil {
					t.Fatal(err)
				}
			}
			canvas := canvasBody(t, "spirax-quotation", []map[string]any{
				controlElement("currency", ""), controlElement("total_amount", ""),
				tableElement([][]map[string]any{{controlElement("item_model_0", ""), controlElement("item_qty_0", ""), controlElement("item_price_0", ""), controlElement("item_amount_0", "")}}),
			})
			saved, err := h.mailDB.SaveDocument(ctx, mailstore.DocumentRecord{OwnerID: account.OwnerID, Type: "quotation", Name: "Untouched", HTML: canvas}, 0)
			if err != nil {
				t.Fatal(err)
			}
			app.Post("/api/ai/write-document", h.HandleWriteDocument)
			body, _ := json.Marshal(aiDocumentInput{AccountEmail: account.Email, Mode: "variables", DocumentType: "quotation", Canvas: canvas, Instruction: "按用户指定的美元价格创建报价：DP27 DN40，1台3714美元；SV607 DN40/DN65，1台1350美元；来源参考 prod_external_dp27。"})
			req := httptest.NewRequest("POST", "/api/ai/write-document", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			res, err := app.Test(req, 5000)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, _ := io.ReadAll(res.Body)
			if calls != 2 {
				t.Fatalf("calls=%d body=%s", calls, raw)
			}
			if mode == "unavailable product remains" {
				if res.StatusCode != 422 || !strings.Contains(string(raw), "第 1 项（DP27 DN40）") {
					t.Fatalf("unhelpful reference failure: %d %s", res.StatusCode, raw)
				}
			} else {
				if res.StatusCode != 200 {
					t.Fatalf("%d %s", res.StatusCode, raw)
				}
				var result struct {
					HTML    string `json:"html"`
					Changed *bool  `json:"changed"`
					Summary string `json:"summary"`
				}
				if err := json.Unmarshal(raw, &result); err != nil {
					t.Fatal(err)
				}
				if mode == "restricted mailbox" {
					if result.Changed == nil || *result.Changed || !strings.Contains(result.Summary, "品牌范围") {
						t.Fatal("brand scope bypassed", string(raw))
					}
				} else {
					values, items, ok := canvasDocumentContent(result.HTML)
					if !ok || len(items) != 2 || values["total_amount"] != "5064.00" || values["currency"] != "USD" {
						t.Fatalf("wrong quote: %v %+v", values, items)
					}
					visible, rows, ok := canvasProjectionContent(result.HTML)
					if !ok || len(rows) != 2 || visible["total_amount"] != "5064.00" || rows[0].Amount != "3714.00" || rows[1].Amount != "1350.00" {
						t.Fatalf("rendered rows differ from quotation data: values=%v rows=%+v", visible, rows)
					}
					for _, item := range items {
						if item.ProductID != "" || item.PriceID != "" || item.VariantID != "" {
							t.Fatal("external reference was attached", item)
						}
					}
				}
			}
			unchanged, err := h.mailDB.GetDocument(ctx, account.OwnerID, saved.ID)
			if err != nil || unchanged.HTML != canvas {
				t.Fatal("proposal wrote saved document", err)
			}
		})
	}
}
