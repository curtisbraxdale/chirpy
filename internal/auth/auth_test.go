package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMakeAndValidateJWT tests the creation and validation of a valid JWT
func TestMakeAndValidateJWT(t *testing.T) {
	testID := uuid.New()
	testSecret := "secrettest"
	testJWT, err := MakeJWT(testID, testSecret, time.Minute*5)
	if err != nil {
		t.Fatalf("Error making JWT: %s", err)
	}
	returnID, err := ValidateJWT(testJWT, testSecret)
	if err != nil {
		t.Fatalf("Error validating JWT: %s", err)
	}
	if returnID != testID {
		t.Fatal("UserID's do not match")
	}
}

func TestExpiredJWT(t *testing.T) {
	testID := uuid.New()
	testSecret := "secrettest"
	testJWT, err := MakeJWT(testID, testSecret, time.Minute*-5)
	if err != nil {
		t.Fatalf("Error making JWT: %s", err)
	}
	_, err = ValidateJWT(testJWT, testSecret)
	if err == nil {
		t.Fatal("Validated expired token.")
	}
}

func TestWrongSecret(t *testing.T) {
	testID := uuid.New()
	testSecret := "secrettest"
	testJWT, err := MakeJWT(testID, testSecret, time.Minute*5)
	if err != nil {
		t.Fatalf("Error making JWT: %s", err)
	}
	_, err = ValidateJWT(testJWT, "wrongsecret")
	if err == nil {
		t.Fatal("Validated wrong secret.")
	}
}

// You can add more test functions here for the other scenarios (expired tokens, wrong secret)
// func TestExpiredJWT(t *testing.T) { ... }
// func TestWrongSecretJWT(t *testing.T) { ... }
