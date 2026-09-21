package identity

import "testing"

func TestPasswordRoundtrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password must verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password must not verify")
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	a, _ := HashPassword("same password")
	b, _ := HashPassword("same password")
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyPasswordRejectsGarbage(t *testing.T) {
	for _, h := range []string{"", "not-a-hash", "$argon2id$v=19$broken", "$bcrypt$whatever$x$y"} {
		if VerifyPassword(h, "anything") {
			t.Fatalf("garbage hash %q must not verify", h)
		}
	}
}
