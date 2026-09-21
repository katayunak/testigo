package flowEntity

import "testing"

func TestTenantSchemeFindsAColumnThatLeadsUniquenessOnTwoTables(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "logs", Columns: []string{"ledger", "idempotency_key"}, Kind: "unique_index"},
		{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: "unique_index"},
	}}

	got, ok := i.TenantScheme()
	if !ok {
		t.Fatal("expected a tenant scheme to be found")
	}
	if got.Column != "ledger" {
		t.Fatalf("got column %q, want %q", got.Column, "ledger")
	}
	if len(got.Tables) != 2 || got.Tables[0] != "logs" || got.Tables[1] != "transactions" {
		t.Fatalf("got tables %v, want [logs transactions]", got.Tables)
	}
}

func TestTenantSchemeNeedsTwoTablesNotOne(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "logs", Columns: []string{"ledger", "idempotency_key"}, Kind: "unique_index"},
	}}

	if _, ok := i.TenantScheme(); ok {
		t.Fatal("one table sharing the leading column proves nothing about a tenant scheme")
	}
}

func TestTenantSchemeIgnoresAPlainSingleColumnUnique(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "users", Columns: []string{"email"}, Kind: "unique_index"},
		{Table: "accounts", Columns: []string{"email"}, Kind: "unique_index"},
	}}

	if _, ok := i.TenantScheme(); ok {
		t.Fatal("a single-column unique constraint is an ordinary uniqueness rule, not a shared-table tenant discriminator")
	}
}

func TestTenantSchemeIgnoresConstraintsThatArentUnique(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "logs", Columns: []string{"ledger", "id"}, Kind: "primary_key"},
		{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: "primary_key"},
	}}

	if _, ok := i.TenantScheme(); ok {
		t.Fatal("a shared primary key shape is not the same claim as a composite uniqueness constraint")
	}
}

func TestTenantSchemePicksTheColumnWithMoreTables(t *testing.T) {
	i := Infra{Constraints: []Constraint{
		{Table: "logs", Columns: []string{"ledger", "idempotency_key"}, Kind: "unique_index"},
		{Table: "transactions", Columns: []string{"ledger", "id"}, Kind: "unique_index"},
		{Table: "widgets", Columns: []string{"shop_id", "sku"}, Kind: "unique_constraint"},
		{Table: "orders", Columns: []string{"shop_id", "order_id"}, Kind: "unique_constraint"},
		{Table: "carts", Columns: []string{"shop_id", "cart_id"}, Kind: "unique_constraint"},
	}}

	got, ok := i.TenantScheme()
	if !ok {
		t.Fatal("expected a tenant scheme to be found")
	}
	if got.Column != "shop_id" {
		t.Fatalf("got column %q, want %q (it scopes uniqueness on 3 tables against ledger's 2)", got.Column, "shop_id")
	}
}
