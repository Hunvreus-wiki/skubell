#!/usr/bin/env bash
set -euo pipefail

# Seeds a protection-workflow test dataset: 50 content pages and 20 templates
# ("models") with a deliberately tiered transclusion distribution so the
# transclusion-count selection filter (§3.1) can be exercised at its thresholds.
#
#   Templates (20 total):
#     Protect Model 01-03  -> transcluded in 21 pages each  (above a 20-threshold)
#     Protect Model 04-08  -> transcluded in 11 pages each  (above a 10-threshold)
#     Protect Model 09-16  -> transcluded in  1 page each
#     Protect Model 17-20  -> unused (0 transclusions)
#
# Transclusions are spread across all 50 pages so every page has content.

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
    ;;
  8082)
    APP_SERVICE="mw146"
    ;;
  *)
    echo "Unsupported target port '${PORT}'. Expected 8081 or 8082." >&2
    exit 1
    ;;
esac

run_compose_exec() {
  docker compose -f "${COMPOSE_FILE}" exec -T "${APP_SERVICE}" "$@"
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

mw_edit() {
  local title="$1"
  local summary="$2"
  local text="$3"

  printf "%s" "${text}" | run_compose_exec \
    php /var/www/html/maintenance/edit.php \
    --summary "${summary}" \
    --user TestEditor \
    "${title}" >/dev/null
}

# Per-page accumulated transclusion markup, indexed by page number (1..50).
declare -a PAGE_TX

assign_range() { # label, start, end
  local label="$1" start="$2" end="$3" p
  for p in $(seq "${start}" "${end}"); do
    PAGE_TX[p]+="{{${label}}}"$'\n'
  done
}

assign_page() { # label, page
  PAGE_TX[$2]+="{{$1}}"$'\n'
}

# Per-page accumulated inbound-link markup ([[...]]), indexed by page number.
declare -a PAGE_LN

assign_links_range() { # target, start, end
  local target="$1" start="$2" end="$3" p
  for p in $(seq "${start}" "${end}"); do
    PAGE_LN[p]+="* [[${target}]]"$'\n'
  done
}

assign_links_page() { # target, page
  PAGE_LN[$2]+="* [[$1]]"$'\n'
}

seed_data() {
  local n title text label

  mw_script createAndPromote --force TestEditor 'TestEditorPass!'

  # --- Transclusion distribution -------------------------------------------
  # 21-page tier (spread to cover pages 1..50 between the three).
  assign_range "Protect Model 01" 1 21
  assign_range "Protect Model 02" 15 35
  assign_range "Protect Model 03" 30 50
  # 11-page tier.
  assign_range "Protect Model 04" 1 11
  assign_range "Protect Model 05" 11 21
  assign_range "Protect Model 06" 21 31
  assign_range "Protect Model 07" 31 41
  assign_range "Protect Model 08" 40 50
  # 1-page tier (single distinct page each).
  assign_page "Protect Model 09" 5
  assign_page "Protect Model 10" 12
  assign_page "Protect Model 11" 19
  assign_page "Protect Model 12" 26
  assign_page "Protect Model 13" 33
  assign_page "Protect Model 14" 40
  assign_page "Protect Model 15" 47
  assign_page "Protect Model 16" 50
  # Protect Model 17-20: intentionally unused (0 transclusions).

  # --- Inbound-link distribution -------------------------------------------
  # Deliberately DIFFERENT from transclusions (via [[Template:...]] wikilinks, not {{...}}), so the two metrics
  # diverge: the transclusion-unused templates 18-20 become the MOST-linked.
  assign_links_range "Template:Protect Model 18" 1 23   # 23 inbound links
  assign_links_range "Template:Protect Model 19" 14 36  # 23
  assign_links_range "Template:Protect Model 20" 28 50  # 23
  assign_links_range "Template:Protect Model 15" 1 12   # 12
  assign_links_range "Template:Protect Model 16" 20 31  # 12
  assign_links_range "Template:Protect Model 17" 39 50  # 12
  assign_links_page "Template:Protect Model 01" 5       # 2 (heavily transcluded, barely linked)
  assign_links_page "Template:Protect Model 01" 45
  assign_links_page "Template:Protect Model 02" 10      # 2
  assign_links_page "Template:Protect Model 02" 40
  # Protect Model 03-14: no inbound links.

  # --- Templates ("models") ------------------------------------------------
  for n in $(seq 1 20); do
    label=$(printf "Protect Model %02d" "${n}")
    mw_edit "Template:${label}" "Seed protection-workflow template ${n}" \
      "'''${label}''' block for Skubell protection-workflow transclusion tests.<noinclude>
Reusable template (\"model\"). Transclusion count is the selection signal.</noinclude>"
  done

  # --- Content pages -------------------------------------------------------
  for n in $(seq 1 50); do
    title=$(printf "Protect Page %03d" "${n}")
    text="Content page ${n} for Skubell protection-workflow testing."$'\n\n'"${PAGE_TX[n]:-}"
    if [[ -n "${PAGE_LN[n]:-}" ]]; then
      text+=$'\n== See also =='$'\n'"${PAGE_LN[n]}"
    fi
    mw_edit "${title}" "Seed protection-workflow page ${n}" "${text}"
  done

  # Populate cached querypages (Mostlinkedtemplates / Mostlinked) used by the count filters when the wiki runs in
  # miser mode; prop=transcludedin / prop=linkshere work live regardless.
  mw_script updateSpecialPages
}

echo "Seeding protection-workflow data on ${TARGET} via service ${APP_SERVICE}..."
"$(dirname "$0")/wait-for-wiki.sh" "${TARGET}"
seed_data

cat <<SUMMARY
Protection-workflow dataset is ready on ${TARGET}
  Pages:     Protect Page 001..050        (50 content pages)
  Templates: Template:Protect Model 01..20 (20 "models")
Transclusion tiers (distinct pages per template):
  21 pages: Protect Model 01, 02, 03
  11 pages: Protect Model 04, 05, 06, 07, 08
   1 page:  Protect Model 09..16
   unused:  Protect Model 17..20
Inbound-link tiers (distinct pages linking to each template — INVERTED vs transclusions):
  23 links: Protect Model 18, 19, 20   (0 transclusions each)
  12 links: Protect Model 15, 16, 17
   2 links: Protect Model 01, 02
   none:    Protect Model 03..14
SUMMARY
