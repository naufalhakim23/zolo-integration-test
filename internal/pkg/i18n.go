package pkg

import (
	"strings"
)

// Localizer turns a message code plus params into a sentence in the caller's language.
type Localizer struct {
	defaultLang string
}

func NewLocalizer(defaultLang string) *Localizer {
	if _, ok := catalog[defaultLang]; !ok {
		defaultLang = LangEN
	}
	return &Localizer{defaultLang: defaultLang}
}

const (
	LangEN = "en"
	LangMS = "ms"
)

var catalog = map[string]map[string]string{
	LangEN: {
		MsgSyncSuccess:       "Order {order_id} was sent to the ERP successfully.",
		MsgSyncPartial:       "Order {order_id} was partly accepted. These items were rejected: {skus}. Please review them and sync again.",
		MsgSyncFailed:        "Order {order_id} could not be sent to the ERP. {reason}",
		MsgSyncInProgress:    "Order {order_id} is already being synced. Please wait a moment before trying again.",
		MsgOrderNotFound:     "Order {order_id} was not found.",
		MsgOrderNotConfirmed: "Order {order_id} has not been confirmed yet, so it cannot be synced.",
		MsgOrderExpired:      "Order {order_id} was confirmed more than 24 hours ago and the ERP will no longer accept it. Please re-confirm the order.",
		MsgUnknownTenant:     "No ERP mapping is configured for tenant {tenant_id}.",
		MsgInvalidPartnerRef: "The customer reference {external_ref} is not in the format the ERP expects (for example CUST-882).",
		MsgNoLineItems:       "Order {order_id} has no line items to sync.",
		MsgLineRejected:      "{sku} was rejected by the ERP: {reason}",
		MsgERPUnavailable:    "The ERP is not responding right now. The order has not been sent, please try again shortly.",
		MsgERPRejected:       "The ERP rejected order {order_id}: {reason}",
		MsgInternalError:     "Something went wrong on our side. The order has not been sent.",
		MsgInvalidRequest:    "The request could not be understood.",
	},
	LangMS: {
		MsgSyncSuccess:       "Pesanan {order_id} berjaya dihantar ke ERP.",
		MsgSyncPartial:       "Pesanan {order_id} diterima sebahagian sahaja. Item berikut ditolak: {skus}. Sila semak dan segerak semula.",
		MsgSyncFailed:        "Pesanan {order_id} gagal dihantar ke ERP. {reason}",
		MsgSyncInProgress:    "Pesanan {order_id} sedang disegerakkan. Sila tunggu sebentar sebelum mencuba lagi.",
		MsgOrderNotFound:     "Pesanan {order_id} tidak dijumpai.",
		MsgOrderNotConfirmed: "Pesanan {order_id} belum disahkan, jadi ia tidak boleh disegerakkan.",
		MsgOrderExpired:      "Pesanan {order_id} telah disahkan lebih 24 jam yang lalu dan ERP tidak lagi menerimanya. Sila sahkan semula pesanan ini.",
		MsgUnknownTenant:     "Tiada pemetaan ERP dikonfigurasikan untuk tenant {tenant_id}.",
		MsgInvalidPartnerRef: "Rujukan pelanggan {external_ref} tidak mengikut format yang diperlukan ERP (contohnya CUST-882).",
		MsgNoLineItems:       "Pesanan {order_id} tiada item untuk disegerakkan.",
		MsgLineRejected:      "{sku} ditolak oleh ERP: {reason}",
		MsgERPUnavailable:    "ERP tidak memberi respons buat masa ini. Pesanan belum dihantar, sila cuba sebentar lagi.",
		MsgERPRejected:       "ERP menolak pesanan {order_id}: {reason}",
		MsgInternalError:     "Berlaku masalah di pihak kami. Pesanan belum dihantar.",
		MsgInvalidRequest:    "Permintaan tidak dapat difahami.",
	},
}

func (l *Localizer) Translate(lang, code string, params map[string]string) string {
	template, ok := catalog[l.resolve(lang)][code]
	if !ok {
		template, ok = catalog[LangEN][code]
		if !ok {
			return code
		}
	}

	for key, value := range params {
		template = strings.ReplaceAll(template, "{"+key+"}", value)
	}

	return stripPlaceholders(template)
}

// resolve reads only the first Accept-Language tag and ignores q-weights, which is
// enough for a two-language catalog and keeps an RFC 4647 matcher out of the deps.
func (l *Localizer) resolve(header string) string {
	for part := range strings.SplitSeq(header, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if tag == "" {
			continue
		}
		base, _, _ := strings.Cut(tag, "-")
		if _, ok := catalog[base]; ok {
			return base
		}
	}
	return l.defaultLang
}

// stripPlaceholders drops tokens the caller did not supply, so a partly filled
// template still reads as a sentence instead of showing {reason} to an order taker.
func stripPlaceholders(s string) string {
	for {
		open := strings.Index(s, "{")
		if open < 0 {
			break
		}
		closing := strings.Index(s[open:], "}")
		if closing < 0 {
			break
		}
		s = s[:open] + s[open+closing+1:]
	}
	return strings.Join(strings.Fields(s), " ")
}
