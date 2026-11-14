package platform

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"math/big"
	"net/http"
	"strconv"
	"time"
)

func digits(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func claim(c jwt.MapClaims, k string) string {
	switch v := c[k].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return ""
}
func matchClaims(c jwt.MapClaims, repo string, branches []string, allowMR, requireProtected bool) bool {
	if claim(c, "ref_type") != "branch" || claim(c, "project_id") != repo {
		return false
	}
	for _, key := range []string{"job_project_id", "source_project_id"} {
		if v := claim(c, key); v != "" && v != repo {
			return false
		}
	}
	mr := claim(c, "pipeline_source") == "merge_request_event"
	if mr && (!allowMR || claim(c, "job_project_id") != repo) {
		return false
	}
	if requireProtected {
		protected := c["ref_protected"]
		if protected != true && protected != "true" {
			return false
		}
	}
	ref := claim(c, "ref")
	if ref == "" {
		return false
	}
	for _, b := range branches {
		if b == "*" || b == ref {
			return true
		}
	}
	return false
}
func (a *App) checkOIDCCredential(c row) {
	t := a.one("SELECT t.*,i.enabled FROM trusts t JOIN issuers i ON i.id=t.issuer_id WHERE t.id=?", c["trust_id"])
	if t == nil || num(t, "enabled") != 1 || str(t, "project_id") != str(c, "target") {
		fail(403, "trust_revoked", "Publisher trust disabled")
	}
	if num(c, "mr") == 1 && num(t, "allow_mr_pipelines") == 0 {
		fail(403, "mr_disabled", "MR pipelines are disabled for this trust")
	}
}
func (a *App) exchange(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Token     string `json:"token"`
		ProjectID string `json:"project_id"`
	}
	body(r, &b)
	if len(b.Token) > 32768 {
		fail(400, "invalid_token", "Token too large")
	}
	idem := r.Header.Get("Idempotency-Key")
	if len(idem) < 8 || len(idem) > 128 {
		fail(400, "idempotency_required", "Idempotency-Key required")
	}
	parser := jwt.NewParser(jwt.WithJSONNumber())
	unverified, _, e := parser.ParseUnverified(b.Token, jwt.MapClaims{})
	if e != nil {
		fail(401, "invalid_oidc", "Malformed ID token")
	}
	uc := unverified.Claims.(jwt.MapClaims)
	issuer := a.one("SELECT * FROM issuers WHERE url=? AND enabled=1", claim(uc, "iss"))
	if issuer == nil {
		fail(403, "unknown_issuer", "Issuer not configured")
	}
	var jwks struct {
		Keys []struct{ Kty, Kid, N, E, Alg, Use string }
	}
	if e = getJSON(r.Context(), str(issuer, "jwks_url"), "", &jwks); e != nil {
		fail(503, "jwks_unavailable", "Unable to retrieve issuer keys")
	}
	token, e := jwt.Parse(b.Token, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing key id")
		}
		for _, k := range jwks.Keys {
			if k.Kid != kid || k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") || (k.Use != "" && k.Use != "sig") {
				continue
			}
			n, ne := base64.RawURLEncoding.DecodeString(k.N)
			eb, ee := base64.RawURLEncoding.DecodeString(k.E)
			if ne != nil || ee != nil || len(n) < 256 || len(n) > 1024 || len(eb) > 4 {
				return nil, fmt.Errorf("invalid RSA key")
			}
			ev := 0
			for _, v := range eb {
				ev = ev*256 + int(v)
			}
			if ev < 3 || ev%2 == 0 {
				return nil, fmt.Errorf("invalid exponent")
			}
			return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: ev}, nil
		}
		return nil, fmt.Errorf("unknown key")
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(str(issuer, "url")), jwt.WithAudience(a.C.BaseURL), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithJSONNumber())
	if e != nil || !token.Valid {
		fail(401, "invalid_oidc", "Invalid signature, audience or validity period")
	}
	c := token.Claims.(jwt.MapClaims)
	exp, _ := c.GetExpirationTime()
	if exp == nil || exp.Unix() <= now() || claim(c, "jti") == "" || claim(c, "iat") == "" || claim(c, "nbf") == "" {
		fail(401, "invalid_oidc", "Required claims missing")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	issuer = a.one("SELECT * FROM issuers WHERE id=? AND enabled=1", issuer["id"])
	if issuer == nil {
		fail(403, "unknown_issuer", "Issuer disabled")
	}
	project := a.one("SELECT * FROM projects WHERE id=? AND deleted=0 AND disabled=0", b.ProjectID)
	if project == nil {
		fail(404, "not_found", "Project unavailable")
	}
	var trust row
	for _, t := range a.rows("SELECT * FROM trusts WHERE project_id=? AND issuer_id=?", b.ProjectID, issuer["id"]) {
		var branches []string
		json.Unmarshal([]byte(str(t, "branches")), &branches)
		if matchClaims(c, str(t, "repository_id"), branches, num(t, "allow_mr_pipelines") == 1, num(t, "require_ref_protected") == 1) {
			trust = t
			break
		}
	}
	if trust == nil {
		fail(403, "untrusted_publisher", "Repository, source branch or MR policy does not match; MR requires job_project_id and the same source/execution repository")
	}
	old := a.one("SELECT * FROM exchanges WHERE issuer_id=? AND jti=?", issuer["id"], claim(c, "jti"))
	if old != nil {
		cr := a.one("SELECT * FROM credentials WHERE id=? AND revoked=0 AND expires_at>?", old["credential_id"], now())
		if str(old, "idem") != idem || cr == nil || str(cr, "target") != b.ProjectID {
			fail(409, "token_replayed", "ID token already exchanged")
		}
		a.checkOIDCCredential(cr)
		send(w, 200, row{"access_token": old["secret"], "expires_at": old["expires_at"], "token_type": "Bearer"})
		return
	}
	expiry := time.Now().Add(5 * time.Minute).Unix()
	if exp.Unix() < expiry {
		expiry = exp.Unix()
	}
	secret, cid := "hoshilet_oidc_"+id()+id(), id()
	tx, e := a.DB.Begin()
	if e != nil {
		panic(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO credentials(id,hash,kind,scope,target,expires_at,trust_id,mr,created_at) VALUES(?,?,'oidc','project',?,?,?,?,?)", cid, digest(secret), b.ProjectID, expiry, trust["id"], claim(c, "pipeline_source") == "merge_request_event", now()); e != nil {
		panic(e)
	}
	if _, e = tx.Exec("INSERT INTO exchanges VALUES(?,?,?,?,?,?)", issuer["id"], claim(c, "jti"), idem, cid, secret, expiry); e != nil {
		panic(e)
	}
	if e = tx.Commit(); e != nil {
		panic(e)
	}
	a.audit(cid, "oidc.exchange", b.ProjectID)
	send(w, 200, row{"access_token": secret, "expires_at": expiry, "token_type": "Bearer"})
}
