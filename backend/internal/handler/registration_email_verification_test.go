package handler

import "testing"

func TestSecureRegistrationCodeIsSixDigits(t *testing.T) {
	for index := 0; index < 64; index++ {
		code, err := secureRegistrationCode()
		if err != nil {
			t.Fatal(err)
		}
		if !registrationCodePattern.MatchString(code) {
			t.Fatalf("secureRegistrationCode() = %q, want six digits", code)
		}
	}
}

func TestRegistrationCodeDigestBindsEmailAndCode(t *testing.T) {
	h := &handlers{jwtSecret: "0123456789abcdef0123456789abcdef"}
	first := h.registrationCodeDigest("Member@Example.com", "123456")
	if len(first) != 64 {
		t.Fatalf("digest length = %d, want 64", len(first))
	}
	if first != h.registrationCodeDigest("member@example.com", "123456") {
		t.Fatal("normalized email should produce the same digest")
	}
	if first == h.registrationCodeDigest("member@example.com", "654321") {
		t.Fatal("different codes must produce different digests")
	}
	if first == h.registrationCodeDigest("other@example.com", "123456") {
		t.Fatal("different emails must produce different digests")
	}
}

func TestRegistrationRequestIPHashDoesNotExposeAddress(t *testing.T) {
	h := &handlers{jwtSecret: "0123456789abcdef0123456789abcdef"}
	value := h.registrationRequestIPHash("192.0.2.1:43120")
	if len(value) != 64 || value == "192.0.2.1" {
		t.Fatalf("request IP hash = %q", value)
	}
}
