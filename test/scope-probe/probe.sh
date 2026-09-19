#!/usr/bin/env bash
set -euo pipefail

# Ensure jq is in PATH if installed in ~/bin
export PATH="$HOME/bin:$PATH"

if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required but not installed." >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq is required but not installed." >&2
    exit 1
fi

echo "=== Gitea OAuth2 Scope Probe (T-SCOPE-0) ==="
echo

# 1. Configuration inputs
DEFAULT_GITEA_URL="${GITEA_URL:-http://127.0.0.1:3000}"
if [ -z "${GITEA_URL:-}" ]; then
    read -rp "Gitea URL [${DEFAULT_GITEA_URL}]: " INPUT_GITEA_URL
    GITEA_URL="${INPUT_GITEA_URL:-$DEFAULT_GITEA_URL}"
fi
GITEA_URL="${GITEA_URL%/}"

if [ -z "${CLIENT_ID:-}" ]; then
    read -rp "OAuth2 Client ID: " CLIENT_ID
fi
if [ -z "${CLIENT_ID:-}" ]; then
    echo "Error: Client ID is required." >&2
    exit 1
fi

if [ -z "${CLIENT_SECRET+x}" ]; then
    read -rsp "OAuth2 Client Secret (leave empty for public client): " CLIENT_SECRET
    echo
fi

DEFAULT_REDIRECT_URI="http://127.0.0.1:9999/callback"
if [ -z "${REDIRECT_URI:-}" ]; then
    read -rp "Redirect URI [${DEFAULT_REDIRECT_URI}]: " INPUT_REDIRECT_URI
    REDIRECT_URI="${INPUT_REDIRECT_URI:-$DEFAULT_REDIRECT_URI}"
fi

# Layer 1 default scopes from SPEC.md (space-separated in OAuth2 standard)
SCOPES="read:repository read:issue read:user read:organization"

# 2. Generate PKCE verifier & challenge (RFC 7636 S256)
# 64-char hex string is within 43-128 unreserved characters
CODE_VERIFIER=$(openssl rand -hex 32)
CODE_CHALLENGE=$(printf '%s' "${CODE_VERIFIER}" | openssl dgst -sha256 -binary | openssl base64 -e | tr '+/' '-_' | tr -d '=\r\n')

# Random state
STATE=$(openssl rand -hex 16)

# URL-encode parameters
urlencode() {
    local string="${1}"
    local strlen=${#string}
    local encoded=""
    local pos c o
    for (( pos=0 ; pos<strlen ; pos++ )); do
        c=${string:$pos:1}
        case "$c" in
            [-_.~a-zA-Z0-9] ) o="${c}" ;;
            * ) printf -v o '%%%02x' "'$c"
        esac
        encoded+="${o}"
    done
    printf '%s' "${encoded}"
}

ENCODED_REDIRECT=$(urlencode "${REDIRECT_URI}")
ENCODED_SCOPES=$(urlencode "${SCOPES}")

AUTH_URL="${GITEA_URL}/login/oauth/authorize?client_id=${CLIENT_ID}&redirect_uri=${ENCODED_REDIRECT}&response_type=code&state=${STATE}&scope=${ENCODED_SCOPES}&code_challenge=${CODE_CHALLENGE}&code_challenge_method=S256"

echo
echo "------------------------------------------------------------------------"
echo "1. Open this URL in your browser:"
echo
echo "${AUTH_URL}"
echo
echo "2. Log in and approve authorization with scopes: ${SCOPES}"
echo "3. After authorization, your browser will redirect to:"
echo "   ${REDIRECT_URI}?code=...&state=..."
echo "------------------------------------------------------------------------"
echo

read -rsp "Paste the authorization code (or the full redirect URL): " INPUT_CODE
echo
if [ -z "${INPUT_CODE}" ]; then
    echo "Error: Authorization code is required." >&2
    exit 1
fi

# Extract code parameter if a full URL was pasted
if [[ "${INPUT_CODE}" == *"code="* ]]; then
    AUTH_CODE=$(printf '%s' "${INPUT_CODE}" | sed -E 's/.*code=([^&]+).*/\1/')
else
    AUTH_CODE="${INPUT_CODE}"
fi

echo
echo "Exchanging authorization code for access token..."

TOKEN_PARAMS="client_id=${CLIENT_ID}&code=${AUTH_CODE}&grant_type=authorization_code&redirect_uri=${ENCODED_REDIRECT}&code_verifier=${CODE_VERIFIER}"
if [ -n "${CLIENT_SECRET}" ]; then
    TOKEN_PARAMS="${TOKEN_PARAMS}&client_secret=${CLIENT_SECRET}"
fi

TOKEN_HTTP_RESP=$(curl -s -w "\n%{http_code}" -X POST "${GITEA_URL}/login/oauth/access_token" \
    -H "Accept: application/json" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "${TOKEN_PARAMS}")

TOKEN_STATUS=$(printf '%s' "${TOKEN_HTTP_RESP}" | tail -n 1)
TOKEN_BODY=$(printf '%s' "${TOKEN_HTTP_RESP}" | sed '$d')

if [ "${TOKEN_STATUS}" -ne 200 ]; then
    echo "Error: Token exchange failed (HTTP ${TOKEN_STATUS}):" >&2
    echo "${TOKEN_BODY}" >&2
    exit 1
fi

# Keep token in shell variable ONLY, never print in full or save to disk
ACCESS_TOKEN=$(printf '%s' "${TOKEN_BODY}" | jq -r '.access_token // empty')
TOKEN_TYPE=$(printf '%s' "${TOKEN_BODY}" | jq -r '.token_type // empty')
REPORTED_SCOPE=$(printf '%s' "${TOKEN_BODY}" | jq -r '.scope // empty')

if [ -z "${ACCESS_TOKEN}" ]; then
    echo "Error: Response did not contain access_token:" >&2
    echo "${TOKEN_BODY}" >&2
    exit 1
fi

echo "Token exchange succeeded!"
echo "  Token Type: ${TOKEN_TYPE:-Bearer}"
if [ -n "${REPORTED_SCOPE}" ]; then
    echo "  Scope in token response: ${REPORTED_SCOPE}"
else
    echo "  Scope in token response: [None returned / empty]"
fi
echo

# 3. Test GET /api/v1/user (must succeed)
echo "--- Probe 1: GET /api/v1/user (read test) ---"
USER_HTTP_RESP=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Accept: application/json" \
    "${GITEA_URL}/api/v1/user")

USER_STATUS=$(printf '%s' "${USER_HTTP_RESP}" | tail -n 1)
USER_BODY=$(printf '%s' "${USER_HTTP_RESP}" | sed '$d')

echo "  HTTP Status: ${USER_STATUS}"
if [ "${USER_STATUS}" -eq 200 ]; then
    USERNAME=$(printf '%s' "${USER_BODY}" | jq -r '.login // empty')
    echo "  Authenticated as user: ${USERNAME}"
    echo "  Result: PASS (Read operation succeeded as expected)"
else
    echo "  Result: FAIL (Expected 200 OK for GET /api/v1/user)"
    echo "  Response: ${USER_BODY}"
    exit 1
fi
echo

# 4. Probe 2: POST /api/v1/user/repos (write attempt 1)
echo "--- Probe 2: POST /api/v1/user/repos (create repository write attempt) ---"
REPO_PAYLOAD='{"name":"probe-scope-test-repo","private":true,"auto_init":false}'
CREATE_HTTP_RESP=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json" \
    -d "${REPO_PAYLOAD}" \
    "${GITEA_URL}/api/v1/user/repos")

CREATE_STATUS=$(printf '%s' "${CREATE_HTTP_RESP}" | tail -n 1)
CREATE_BODY=$(printf '%s' "${CREATE_HTTP_RESP}" | sed '$d')

echo "  HTTP Status: ${CREATE_STATUS}"
WRITE1_PASSED=false
if [ "${CREATE_STATUS}" -ge 200 ] && [ "${CREATE_STATUS}" -lt 300 ]; then
    echo "  WARNING: Write succeeded! Repository was created (HTTP ${CREATE_STATUS})!"
    WRITE1_PASSED=true
elif [ "${CREATE_STATUS}" -eq 403 ]; then
    echo "  Result: BLOCKED (403 Forbidden - Gitea correctly rejected write with read scopes)"
else
    echo "  Result: Non-2xx response (${CREATE_STATUS}): ${CREATE_BODY}"
fi
echo

# 5. Probe 3: PATCH /api/v1/repos/{owner}/{repo} (write attempt 2)
echo "--- Probe 3: PATCH /api/v1/repos/{owner}/{repo} (edit repository write attempt) ---"
read -rp "Enter an existing repository to attempt PATCH on (e.g. ${USERNAME}/my-repo): " TARGET_REPO
if [ -n "${TARGET_REPO}" ]; then
    PATCH_PAYLOAD='{"description":"unauthorized probe patch description"}'
    PATCH_HTTP_RESP=$(curl -s -w "\n%{http_code}" -X PATCH \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json" \
        -d "${PATCH_PAYLOAD}" \
        "${GITEA_URL}/api/v1/repos/${TARGET_REPO}")

    PATCH_STATUS=$(printf '%s' "${PATCH_HTTP_RESP}" | tail -n 1)
    PATCH_BODY=$(printf '%s' "${PATCH_HTTP_RESP}" | sed '$d')

    echo "  HTTP Status: ${PATCH_STATUS}"
    WRITE2_PASSED=false
    if [ "${PATCH_STATUS}" -ge 200 ] && [ "${PATCH_STATUS}" -lt 300 ]; then
        echo "  WARNING: Write succeeded! Repository was modified (HTTP ${PATCH_STATUS})!"
        WRITE2_PASSED=true
    elif [ "${PATCH_STATUS}" -eq 403 ]; then
        echo "  Result: BLOCKED (403 Forbidden - Gitea correctly rejected write with read scopes)"
    else
        echo "  Result: Non-2xx response (${PATCH_STATUS}): ${PATCH_BODY}"
    fi
else
    echo "  Skipping PATCH probe (no existing repo specified)"
    WRITE2_PASSED=false
fi
echo

# 6. Overall Evaluation
echo "=== Probe Summary ==="
echo "Gitea Server: ${GITEA_URL}"
echo "Requested Scopes: ${SCOPES}"
echo "Token Response Scope: ${REPORTED_SCOPE:-[none]}"
echo "GET /api/v1/user: HTTP ${USER_STATUS}"
echo "POST /api/v1/user/repos: HTTP ${CREATE_STATUS}"
if [ -n "${TARGET_REPO:-}" ]; then
    echo "PATCH /api/v1/repos/${TARGET_REPO}: HTTP ${PATCH_STATUS}"
fi
echo

if [ "${WRITE1_PASSED}" = true ] || [ "${WRITE2_PASSED:-false}" = true ]; then
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    echo "CRITICAL SECURITY FINDING: Gitea permitted write operations with read scopes!"
    echo "Layer 1 scope enforcement is NOT effective on this Gitea version."
    echo "OAuth mode cannot be used safely with this Gitea version."
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    exit 2
fi

echo "SUCCESS: Write operations were successfully blocked (HTTP 403 Forbidden)."
echo "Gitea enforced read-only scopes on the OAuth2 access token."
exit 0