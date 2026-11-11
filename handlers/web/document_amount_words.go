package web

import (
	"strconv"
	"strings"
)

// Preserve authored wording while its numerical basis is unchanged. Otherwise
// derive English invoice wording from the same rounded decimal as the total.
func refreshDocumentAmountWords(values map[string]string, previousTotal string) {
	previous, oldErr := documentNumber(previousTotal)
	current, newErr := documentNumber(values["total_amount"])
	if newErr != nil {
		values["say_total"] = ""
		return
	}
	if strings.TrimSpace(values["say_total"]) != "" && oldErr == nil && previous.Cmp(current) == 0 {
		return
	}
	parts := strings.Split(current.FloatString(2), ".")
	whole := documentIntegerWords(parts[0])
	if whole == "" {
		values["say_total"] = ""
		return
	}
	words := []string{"SAY TOTAL"}
	if currency := documentCurrency(values["currency"]); currency != "" {
		words = append(words, currency)
	}
	words = append(words, whole)
	// A fraction is currency-neutral (CNY has fen, USD has cents).
	if parts[1] != "00" {
		words = append(words, "AND", parts[1]+"/100")
	}
	values["say_total"] = strings.Join(append(words, "ONLY"), " ")
}

func documentIntegerWords(digits string) string {
	small := []string{"ZERO", "ONE", "TWO", "THREE", "FOUR", "FIVE", "SIX", "SEVEN", "EIGHT", "NINE", "TEN", "ELEVEN", "TWELVE", "THIRTEEN", "FOURTEEN", "FIFTEEN", "SIXTEEN", "SEVENTEEN", "EIGHTEEN", "NINETEEN"}
	tens := []string{"", "", "TWENTY", "THIRTY", "FORTY", "FIFTY", "SIXTY", "SEVENTY", "EIGHTY", "NINETY"}
	scales := []string{"", "THOUSAND", "MILLION", "BILLION", "TRILLION", "QUADRILLION", "QUINTILLION", "SEXTILLION", "SEPTILLION", "OCTILLION", "NONILLION", "DECILLION"}
	groups := (len(digits) + 2) / 3
	if groups > len(scales) {
		return ""
	}
	var words []string
	for group := groups - 1; group >= 0; group-- {
		end := len(digits) - group*3
		start := max(0, end-3)
		n, _ := strconv.Atoi(digits[start:end])
		if n == 0 {
			continue
		}
		if n >= 100 {
			words = append(words, small[n/100], "HUNDRED")
			n %= 100
		}
		if n >= 20 {
			words = append(words, tens[n/10])
			n %= 10
		}
		if n > 0 {
			words = append(words, small[n])
		}
		if scales[group] != "" {
			words = append(words, scales[group])
		}
	}
	if len(words) == 0 {
		return "ZERO"
	}
	return strings.Join(words, " ")
}
