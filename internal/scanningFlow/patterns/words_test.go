package patterns

import "testing"

func TestWordsSplitsGoIdentifiers(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"TotalPages", []string{"total", "pages"}},
		{"total_pages", []string{"total", "pages"}},
		{"CurrentPage", []string{"current", "page"}},
		{"Price", []string{"price"}},
		{"HTTPStatus", []string{"http", "status"}},
		{"ReverseDiscountJob", []string{"reverse", "discount", "job"}},
	} {
		got := Words(c.in)
		if len(got) != len(c.want) {
			t.Errorf("Words(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Words(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestCurrentIsNotACurrency(t *testing.T) {
	if IsCurrencyName("CurrentPage") {
		t.Error("CurrentPage was read as a currency field, which is how a pagination struct got scored as money")
	}
	if IsCurrencyName("Current") {
		t.Error("Current is not a currency")
	}
	for _, ok := range []string{"Currency", "CurrencyCode", "CCY", "curr", "ISO4217"} {
		if !IsCurrencyName(ok) {
			t.Errorf("%s should be recognised as a currency field", ok)
		}
	}
}

func TestACountIsNotAnAmount(t *testing.T) {
	for _, n := range []string{"TotalPages", "TotalCount", "PageSize", "RetryCount", "ItemCount"} {
		if !IsCounterName(n) {
			t.Errorf("%s counts things; treating it as money puts a page number top of the money list", n)
		}
	}
	for _, n := range []string{"Price", "Fee", "Amount", "Balance", "TotalAmount"} {
		if IsCounterName(n) {
			t.Errorf("%s is money, not a count", n)
		}
	}
}

func TestPaginationStructsHoldNoMoney(t *testing.T) {
	for _, n := range []string{"Paging", "PaginationResponse", "PageMeta", "Cursor"} {
		if !IsContainerName(n) {
			t.Errorf("%s carries counters, not money", n)
		}
	}
	if IsContainerName("Order") {
		t.Error("Order is the entity that carries money")
	}
}
