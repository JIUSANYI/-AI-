package main

import "testing"

func TestPhonePattern(t *testing.T) {
	for _, phone := range []string{"13800138000", "19912345678"} {
		if !phonePattern.MatchString(phone) {
			t.Errorf("phone %q should be accepted", phone)
		}
	}
	for _, phone := range []string{"1380013800", "12800138000", "13800138000x"} {
		if phonePattern.MatchString(phone) {
			t.Errorf("phone %q should be rejected", phone)
		}
	}
}

func TestSplitSQLStatements(t *testing.T) {
	statements := splitSQLStatements("CREATE TABLE a (id INT);\n\nCREATE TABLE b (id INT);\n")
	if len(statements) != 2 {
		t.Fatalf("got %d statements, want 2", len(statements))
	}
}

func TestHashTokenIsDeterministicAndNonEmpty(t *testing.T) {
	first := hashToken("token")
	if first == "" || first != hashToken("token") || first == hashToken("other") {
		t.Fatal("token hash is not deterministic")
	}
}
