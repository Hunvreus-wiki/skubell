#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <host:port>" >&2
  exit 1
fi

TARGET="$1"
PORT="${TARGET##*:}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.test.yml}"

case "${PORT}" in
  8081)
    APP_SERVICE="mw143"
    DB_SERVICE="mw143-db"
    DB_PASSWORD="wikipass143"
    SYSOP_PASSWORD="WikiSysopPass143!"
    BOTPASS_TESTADMIN='ovgj07dt13opeuti773d17i96hamrg7g'
    BOTPASS_TESTADMIN_IFACE='iface0edit0grant0testadmin0mw143'
    ;;
  8082)
    APP_SERVICE="mw146"
    DB_SERVICE="mw146-db"
    DB_PASSWORD="wikipass146"
    SYSOP_PASSWORD="WikiSysopPass146!"
    BOTPASS_TESTADMIN='r7elmkikmc1mqehngiqo8rrqcs2kktpu'
    BOTPASS_TESTADMIN_IFACE='iface0edit0grant0testadmin0mw146'
    ;;
  *)
    echo "Unsupported target port '${PORT}'. Expected 8081 or 8082." >&2
    exit 1
    ;;
esac

API_URL="http://${TARGET}/api.php"
BOT_NAME="SkubellTest"

# Separate bot password app with interface-edit rights, for editing/deleting
# MediaWiki: namespace pages (needs the editinterface right; editsiteconfig is
# included for MediaWiki:*.css/*.js, which additionally requires the account to
# be in the interface-admin group).
IFACE_BOT_NAME="SkubellIface"
IFACE_GRANTS="basic,highvolume,delete,editinterface,editsiteconfig"

# Bot passwords must be at least 32 characters AND match ^[0-9a-w]{32,}$: MediaWiki's
# BotPassword::canonicalizeLoginData only treats a login as a bot-password login when the
# password is in that charset (digits 0-9, lowercase a-w). A password with uppercase, x/y/z,
# underscores, etc. is stored fine but silently rejected at login as "Incorrect username or
# password". See the guard in create_or_update_botpassword() below.
BOTPASS_TESTEDITOR='testeditor00botpass00skubell0002'
BOTPASS_TESTBLOCKED='testblocked0botpass00skubell0003'
BOTPASS_TESTPARTIAL='testpartial0botpass00skubell0004'

run_compose_exec() {
  docker compose -f "${COMPOSE_FILE}" exec -T "${APP_SERVICE}" "$@"
}

run_db_exec() {
  docker compose -f "${COMPOSE_FILE}" exec -T "${DB_SERVICE}" mariadb -uwikiuser "-p${DB_PASSWORD}" -D mediawiki -e "$1"
}

mw_script() {
  local script_name="$1"
  shift

  if run_compose_exec test -f /var/www/html/maintenance/run.php; then
    run_compose_exec php /var/www/html/maintenance/run.php "${script_name}" "$@"
  else
    run_compose_exec php "/var/www/html/maintenance/${script_name}.php" "$@"
  fi
}

api_get_login_token() {
  local cookie_jar="$1"
  curl -fsS -c "${cookie_jar}" -b "${cookie_jar}" \
    "${API_URL}?action=query&meta=tokens&type=login&format=json" | jq -r '.query.tokens.logintoken'
}

api_login() {
  local user="$1"
  local password="$2"
  local cookie_jar="$3"
  local login_token="$4"

  local response
  response="$(curl -fsS -c "${cookie_jar}" -b "${cookie_jar}" -X POST "${API_URL}" \
    --data-urlencode "action=login" \
    --data-urlencode "format=json" \
    --data-urlencode "lgname=${user}" \
    --data-urlencode "lgpassword=${password}" \
    --data-urlencode "lgtoken=${login_token}")"

  if [[ "${response}" != *'"result":"Success"'* ]]; then
    echo "Login failed for ${user}: ${response}" >&2
    exit 1
  fi
}

api_get_csrf_token() {
  local cookie_jar="$1"
  curl -fsS -b "${cookie_jar}" "${API_URL}?action=query&meta=tokens&type=csrf&format=json" | jq -r '.query.tokens.csrftoken'
}

api_post() {
  local cookie_jar="$1"
  shift
  local response
  response="$(curl -fsS -b "${cookie_jar}" -X POST "${API_URL}" "$@")"
  if [[ "${response}" == *'"error"'* ]]; then
    echo "API request failed: ${response}" >&2
    return 1
  fi
}

create_or_update_user() {
  local username="$1"
  local password="$2"
  shift 2

  mw_script createAndPromote --force "$@" "${username}" "${password}"
}

create_or_update_botpassword() {
  local username="$1"
  local botpassword="$2"
  local appid="${3:-${BOT_NAME}}"
  local grants="${4:-basic,highvolume,delete,protect,createeditmovepage}"

  # Fail loudly on a bot password MediaWiki would silently reject at login.
  # (BotPassword::canonicalizeLoginData requires ^[0-9a-w]{32,}$.)
  if [[ ! "${botpassword}" =~ ^[0-9a-w]{32,}$ ]]; then
    echo "Invalid bot password for ${username}@${appid}: must match ^[0-9a-w]{32,}\$ (digits 0-9 and lowercase a-w, >=32 chars). Got: '${botpassword}'" >&2
    exit 1
  fi

  # Ensure deterministic credentials across repeated provisioning runs. Scope the
  # removal to this appid only: do NOT use `invalidateBotPasswords --user`, which
  # invalidates every bot password the user has (it would clobber other apps such
  # as ${IFACE_BOT_NAME}).
  run_db_exec "DELETE bp FROM bot_passwords bp INNER JOIN user u ON u.user_id = bp.bp_user WHERE u.user_name = '${username}' AND bp.bp_app_id = '${appid}'"
  mw_script createBotPassword --appid "${appid}" --grants "${grants}" "${username}" "${botpassword}" >/dev/null
}

create_seed_pages() {
  local cookie_jar="$1"
  local csrf_token="$2"
  local text

  post_edit() {
    local title="$1"
    local body="$2"
    local summary="$3"

    api_post "${cookie_jar}" \
      --data-urlencode "action=edit" \
      --data-urlencode "format=json" \
      --data-urlencode "title=${title}" \
      --data-urlencode "text=${body}" \
      --data-urlencode "summary=${summary}" \
      --data-urlencode "token=${csrf_token}" \
      --data-urlencode "bot=1"
  }

  # Category pages.
  post_edit "Category:Fruits" "Pages about fruits used in Skubell integration tests." "Seed fruit category"
  post_edit "Category:Colours" "Pages about colours used in Skubell integration tests." "Seed colour category"

  # List pages.
  text=$'* [[red]]\n* [[green]]\n* [[yellow]]\n* [[orange]]\n* [[purple]]'
  post_edit "List of colours" "${text}" "Seed list of colours"
  text=$'* [[apple]]\n* [[banana]]\n* [[orange (fruit)|orange]]\n* [[grape]]'
  post_edit "List of fruits" "${text}" "Seed list of fruits"

  # Colour pages with RGB values and links to fruits.
  text=$'Red is a colour with RGB code \'\'\'255, 0, 0\'\'\'.\nIt can be seen on [[apple]] and [[grape]].\n\n[[Category:Colours]]'
  post_edit "Red" "${text}" "Seed colour page Red"
  text=$'Green is a colour with RGB code \'\'\'0, 128, 0\'\'\'.\nIt can be seen on [[apple]], [[banana]], [[orange (fruit)|orange]], and [[grape]].\n\n[[Category:Colours]]'
  post_edit "Green" "${text}" "Seed colour page Green"
  text=$'Yellow is a colour with RGB code \'\'\'255, 255, 0\'\'\'.\nIt can be seen on [[apple]] and [[banana]].\n\n[[Category:Colours]]'
  post_edit "Yellow" "${text}" "Seed colour page Yellow"
  text=$'Orange is a colour with RGB code \'\'\'255, 165, 0\'\'\'.\nIt can be seen on [[orange (fruit)|orange]].\n\n[[Category:Colours]]'
  post_edit "Orange" "${text}" "Seed colour page Orange"
  text=$'Purple is a colour with RGB code \'\'\'128, 0, 128\'\'\'.\nIt can be seen on [[grape]].\n\n[[Category:Colours]]'
  post_edit "Purple" "${text}" "Seed colour page Purple"

  # Fruit pages linking to colour pages (lowercase wikilinks by request).
  text=$'An apple may be [[red]], [[green]], or [[yellow]].\n\n[[Category:Fruits]]'
  post_edit "Apple" "${text}" "Seed fruit page Apple"
  text=$'A banana is usually [[yellow]] and can be [[green]] when unripe.\n\n[[Category:Fruits]]'
  post_edit "Banana" "${text}" "Seed fruit page Banana"
  text=$'An orange may be [[orange]] and sometimes [[green]].\n\n[[Category:Fruits]]'
  post_edit "Orange (fruit)" "${text}" "Seed fruit page Orange"
  text=$'Grapes can be [[green]], [[red]], or [[purple]].\n\n[[Category:Fruits]]'
  post_edit "Grape" "${text}" "Seed fruit page Grape"

  # Extra pages in Talk and Project namespaces for broader integration coverage.
  api_post "${cookie_jar}" \
    --data-urlencode "action=edit" \
    --data-urlencode "format=json" \
    --data-urlencode "title=Talk:Apple" \
    --data-urlencode "text=Talk page for the Apple test article." \
    --data-urlencode "summary=Seed talk page" \
    --data-urlencode "token=${csrf_token}" \
    --data-urlencode "bot=1"

  api_post "${cookie_jar}" \
    --data-urlencode "action=edit" \
    --data-urlencode "format=json" \
    --data-urlencode "title=Project:Fruit policy" \
    --data-urlencode "text=Project namespace page used by Skubell integration tests." \
    --data-urlencode "summary=Seed project page" \
    --data-urlencode "token=${csrf_token}" \
    --data-urlencode "bot=1"
}

apply_blocks() {
  local cookie_jar="$1"
  local csrf_token="$2"

  api_post "${cookie_jar}" \
    --data-urlencode "action=unblock" \
    --data-urlencode "format=json" \
    --data-urlencode "user=TestBlocked" \
    --data-urlencode "reason=Reset test block state" \
    --data-urlencode "token=${csrf_token}" || true

  api_post "${cookie_jar}" \
    --data-urlencode "action=unblock" \
    --data-urlencode "format=json" \
    --data-urlencode "user=TestPartial" \
    --data-urlencode "reason=Reset test block state" \
    --data-urlencode "token=${csrf_token}" || true

  api_post "${cookie_jar}" \
    --data-urlencode "action=block" \
    --data-urlencode "format=json" \
    --data-urlencode "user=TestBlocked" \
    --data-urlencode "expiry=infinite" \
    --data-urlencode "reason=Skubell integration test: sitewide block" \
    --data-urlencode "nocreate=1" \
    --data-urlencode "autoblock=1" \
    --data-urlencode "token=${csrf_token}"

  api_post "${cookie_jar}" \
    --data-urlencode "action=block" \
    --data-urlencode "format=json" \
    --data-urlencode "user=TestPartial" \
    --data-urlencode "expiry=infinite" \
    --data-urlencode "reason=Skubell integration test: partial block (ns0)" \
    --data-urlencode "partial=1" \
    --data-urlencode "namespacerestrictions=0" \
    --data-urlencode "allowusertalk=1" \
    --data-urlencode "token=${csrf_token}"
}

echo "Provisioning ${TARGET} via service '${APP_SERVICE}' ..."
"$(dirname "$0")/wait-for-wiki.sh" "${TARGET}"

echo "Creating/updating test users ..."
# Convenience local login account for manual browser testing.
create_or_update_user admin 'otempssuspendstonvol' --sysop --bureaucrat
create_or_update_user TestAdmin 'TestAdminPass!' --sysop --bureaucrat
create_or_update_user TestEditor 'TestEditorPass!'
create_or_update_user TestBlocked 'TestBlockedPass!' --sysop
create_or_update_user TestPartial 'TestPartialPass!' --sysop

echo "Creating/updating bot passwords ..."
create_or_update_botpassword TestAdmin "${BOTPASS_TESTADMIN}"
create_or_update_botpassword TestEditor "${BOTPASS_TESTEDITOR}"
create_or_update_botpassword TestBlocked "${BOTPASS_TESTBLOCKED}"
create_or_update_botpassword TestPartial "${BOTPASS_TESTPARTIAL}"
# Interface-edit bot password (separate app id) for MediaWiki: namespace edits/deletions.
create_or_update_botpassword TestAdmin "${BOTPASS_TESTADMIN_IFACE}" "${IFACE_BOT_NAME}" "${IFACE_GRANTS}"

cookie_jar="$(mktemp)"
trap 'rm -f "${cookie_jar}"' EXIT

echo "Applying API seed data and block state ..."
login_token="$(api_get_login_token "${cookie_jar}")"
api_login "WikiSysop" "${SYSOP_PASSWORD}" "${cookie_jar}" "${login_token}"
csrf_token="$(api_get_csrf_token "${cookie_jar}")"
create_seed_pages "${cookie_jar}" "${csrf_token}"
apply_blocks "${cookie_jar}" "${csrf_token}"

cat <<SUMMARY
Provisioning complete for ${TARGET}
Bot password credentials:
  - TestAdmin@SkubellTest   / ${BOTPASS_TESTADMIN}
  - TestEditor@SkubellTest  / ${BOTPASS_TESTEDITOR}
  - TestBlocked@SkubellTest / ${BOTPASS_TESTBLOCKED}
  - TestPartial@SkubellTest / ${BOTPASS_TESTPARTIAL}
  - TestAdmin@${IFACE_BOT_NAME}  / ${BOTPASS_TESTADMIN_IFACE}  (interface-edit: editinterface + delete)
SUMMARY
