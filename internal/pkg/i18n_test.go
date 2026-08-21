package pkg_test

import (
	"strings"
	"testing"

	"zolo-test-integration/internal/app/payload"
	"zolo-test-integration/internal/pkg"
)

func TestLocalizerTranslate(t *testing.T) {
	cases := []struct {
		name   string
		lang   string
		code   string
		params map[string]string
		want   string
	}{
		{
			name:   "english success",
			lang:   pkg.LangEN,
			code:   pkg.MsgSyncSuccess,
			params: map[string]string{"order_id": "ord_1"},
			want:   "Order ord_1 was sent to the ERP successfully.",
		},
		{
			name:   "malay success",
			lang:   pkg.LangMS,
			code:   pkg.MsgSyncSuccess,
			params: map[string]string{"order_id": "ord_1"},
			want:   "Pesanan ord_1 berjaya dihantar ke ERP.",
		},
		{
			name:   "multiple placeholders",
			lang:   pkg.LangEN,
			code:   pkg.MsgSyncPartial,
			params: map[string]string{"order_id": "ord_1", "skus": "SKU-2, SKU-3"},
			want:   "Order ord_1 was partly accepted. These items were rejected: SKU-2, SKU-3. Please review them and sync again.",
		},
		{
			// A template with no params left in it must still read as a sentence.
			name: "missing param is stripped, not shown",
			lang: pkg.LangEN,
			code: pkg.MsgSyncFailed,
			want: "Order could not be sent to the ERP.",
		},
		{
			name: "unknown code falls back to the code itself",
			lang: pkg.LangEN,
			code: "sync.does_not_exist",
			want: "sync.does_not_exist",
		},
	}

	localizer := pkg.NewLocalizer(pkg.LangEN)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := localizer.Translate(c.lang, c.code, c.params); got != c.want {
				t.Errorf("Translate(%q, %q) = %q, want %q", c.lang, c.code, got, c.want)
			}
		})
	}
}

// The Accept-Language header arrives in many shapes; an unsupported one falls back
// rather than showing a raw code.
func TestLocalizerResolvesAcceptLanguage(t *testing.T) {
	cases := []struct {
		name        string
		defaultLang string
		header      string
		wantMalay   bool
	}{
		{name: "plain tag", defaultLang: pkg.LangEN, header: "ms", wantMalay: true},
		{name: "regional tag", defaultLang: pkg.LangEN, header: "ms-MY", wantMalay: true},
		{name: "with q-weights", defaultLang: pkg.LangEN, header: "ms-MY,ms;q=0.9,en;q=0.8", wantMalay: true},
		{name: "uppercase", defaultLang: pkg.LangEN, header: "MS", wantMalay: true},
		{name: "unsupported falls back to default", defaultLang: pkg.LangEN, header: "fr-FR"},
		{name: "empty header uses default", defaultLang: pkg.LangEN, header: ""},
		{name: "unsupported default becomes english", defaultLang: "fr", header: ""},
		{name: "first supported tag wins", defaultLang: pkg.LangEN, header: "fr,ms", wantMalay: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pkg.NewLocalizer(c.defaultLang).
				Translate(c.header, pkg.MsgSyncSuccess, map[string]string{"order_id": "ord_1"})

			if isMalay := strings.HasPrefix(got, "Pesanan"); isMalay != c.wantMalay {
				t.Errorf("Translate(%q) = %q, malay = %v, want %v", c.header, got, isMalay, c.wantMalay)
			}
		})
	}
}

// Every code in the catalog must exist in every language, or a caller silently drops
// to English on one message and not the next.
func TestCatalogCoverage(t *testing.T) {
	codes := []string{
		pkg.MsgSyncSuccess, pkg.MsgSyncPartial, pkg.MsgSyncFailed, pkg.MsgSyncInProgress,
		pkg.MsgOrderNotFound, pkg.MsgOrderNotConfirmed, pkg.MsgOrderExpired,
		pkg.MsgOrderExists, pkg.MsgOrderCreated,
		pkg.MsgUnknownTenant, pkg.MsgInvalidPartnerRef, pkg.MsgNoLineItems,
		pkg.MsgLineRejected, pkg.MsgERPUnavailable, pkg.MsgERPRejected,
		pkg.MsgInternalError, pkg.MsgInvalidRequest,
	}

	localizer := pkg.NewLocalizer(pkg.LangEN)

	for _, lang := range []string{pkg.LangEN, pkg.LangMS} {
		for _, code := range codes {
			t.Run(lang+"/"+code, func(t *testing.T) {
				got := localizer.Translate(lang, code, nil)
				if got == code {
					t.Errorf("%s has no %s translation", code, lang)
				}
				if strings.ContainsAny(got, "{}") {
					t.Errorf("%s rendered with an unresolved placeholder: %q", code, got)
				}
			})
		}
	}
}

func TestSyncResultLocalize(t *testing.T) {
	cases := []struct {
		name        string
		result      payload.SyncResult
		lang        string
		wantMessage string
	}{
		{
			name:        "order id is filled in from the result",
			result:      payload.SyncResult{OrderID: "ord_1", MessageCode: pkg.MsgSyncSuccess},
			lang:        pkg.LangEN,
			wantMessage: "Order ord_1 was sent to the ERP successfully.",
		},
		{
			name: "explicit params win over the result order id",
			result: payload.SyncResult{
				OrderID:     "ord_1",
				MessageCode: pkg.MsgSyncSuccess,
				Params:      map[string]string{"order_id": "ord_override"},
			},
			lang:        pkg.LangEN,
			wantMessage: "Order ord_override was sent to the ERP successfully.",
		},
		{
			name:        "no message code leaves the message empty",
			result:      payload.SyncResult{OrderID: "ord_1"},
			lang:        pkg.LangEN,
			wantMessage: "",
		},
		{
			name:        "malay caller",
			result:      payload.SyncResult{OrderID: "ord_1", MessageCode: pkg.MsgSyncSuccess},
			lang:        "ms-MY",
			wantMessage: "Pesanan ord_1 berjaya dihantar ke ERP.",
		},
	}

	localizer := pkg.NewLocalizer(pkg.LangEN)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := c.result
			result.Localize(localizer, c.lang)

			if result.Message != c.wantMessage {
				t.Errorf("Message = %q, want %q", result.Message, c.wantMessage)
			}
		})
	}
}
