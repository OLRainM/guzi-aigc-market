package auth

import (
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	service := Service{cfg: Config{JWTSecret: "a-development-secret-with-32-characters", Issuer: "test", AccessTTL: time.Minute}}
	user := User{ID: "user-1", Roles: []Role{{Code: "USER"}}}
	raw, err := service.createAccessToken(&user)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.parseAccessToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != user.ID || len(claims.Roles) != 1 || claims.Roles[0] != "USER" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestRefreshTokenHash(t *testing.T) {
	first, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("random tokens must differ")
	}
	if tokenHash(first) != tokenHash(first) {
		t.Fatal("token hash must be stable")
	}
	if tokenHash(first) == first {
		t.Fatal("stored hash must not expose raw token")
	}
}
