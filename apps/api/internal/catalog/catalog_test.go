package catalog

import "testing"

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name    string
		request productRequest
		valid   bool
	}{
		{name: "valid sale", request: productRequest{Title: "限定模型", Description: "完整描述", PriceCents: 12900, IPName: "原创", Category: "手办", Condition: "全新", Stock: 1}, valid: true},
		{name: "valid preorder", request: productRequest{Title: "预售模型", Description: "预计三个月发货", PriceCents: 25900, IPName: "原创", Category: "雕像", Condition: "全新", TransactionType: "preorder", Stock: 10}, valid: true},
		{name: "empty title", request: productRequest{Description: "完整描述", PriceCents: 12900, IPName: "原创", Category: "手办", Condition: "全新", Stock: 1}, valid: false},
		{name: "invalid price", request: productRequest{Title: "限定模型", Description: "完整描述", PriceCents: 0, IPName: "原创", Category: "手办", Condition: "全新", Stock: 1}, valid: false},
		{name: "invalid transaction type", request: productRequest{Title: "限定模型", Description: "完整描述", PriceCents: 12900, IPName: "原创", Category: "手办", Condition: "全新", TransactionType: "AUCTION", Stock: 1}, valid: false},
		{name: "negative stock", request: productRequest{Title: "限定模型", Description: "完整描述", PriceCents: 12900, IPName: "原创", Category: "手办", Condition: "全新", Stock: -1}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRequest(&test.request)
			if (err == nil) != test.valid {
				t.Fatalf("validateRequest() error = %v, want valid = %v", err, test.valid)
			}
		})
	}
}

func TestValidTransition(t *testing.T) {
	tests := []struct {
		from, to string
		valid    bool
	}{
		{StatusDraft, StatusPublished, true},
		{StatusPublished, StatusOffShelf, true},
		{StatusDraft, StatusOffShelf, false},
		{StatusPublished, StatusDraft, false},
		{StatusOffShelf, StatusPublished, true},
	}
	for _, test := range tests {
		if got := validTransition(test.from, test.to); got != test.valid {
			t.Errorf("validTransition(%q, %q) = %v, want %v", test.from, test.to, got, test.valid)
		}
	}
}
