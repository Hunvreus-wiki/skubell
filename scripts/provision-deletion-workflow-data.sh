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

seed_data() {
  local i title text

  mw_script createAndPromote --force TestEditor 'TestEditorPass!'

  mw_edit "Category:KWF Shared Category" \
    "Seed KWF shared category" \
    "Shared category for Skubell deletion workflow testing."

  mw_edit "Category:KWF Creator Batch" \
    "Seed KWF creator category" \
    "Pages created by TestEditor for creator-based filtering tests."

  mw_edit "Template:KWF Banner" \
    "Seed KWF template" \
    "This page uses the KWF banner template for transclusion tests."

  text="== KWF Link Hub =="
  for i in $(seq -w 1 10); do
    text+=$'\n* [[KWF Creator '"${i}"']]'
  done
  mw_edit "KWF Link Hub" "Seed KWF link hub" "${text}"

  for i in $(seq -w 1 12); do
    title="KWF Creator ${i}"
    text="Creator criterion dataset page ${i}.

This page was created by TestEditor.
{{KWF Banner}}
[[Category:KWF Shared Category]]
[[Category:KWF Creator Batch]]"
    mw_edit "${title}" "Seed KWF creator dataset page ${i}" "${text}"
  done

  for i in $(seq -w 1 12); do
    title="KWF PrefixAlpha ${i}"
    text="Prefix criterion dataset page ${i}. [[Category:KWF Shared Category]]"
    mw_edit "${title}" "Seed KWF prefix dataset page ${i}" "${text}"
  done

  for i in $(seq -w 1 10); do
    title="Project:KWF Namespace ${i}"
    text="Namespace criterion page ${i} in Project namespace."
    mw_edit "${title}" "Seed KWF project namespace page ${i}" "${text}"
  done

  for i in $(seq -w 1 10); do
    title="KWF Redirect Source ${i}"
    text="#REDIRECT [[KWF Creator ${i}]]"
    mw_edit "${title}" "Seed KWF redirect page ${i}" "${text}"
  done

  for i in $(seq -w 1 10); do
    title="KWF Broken Redirect ${i}"
    text="#REDIRECT [[KWF Missing Target ${i}]]"
    mw_edit "${title}" "Seed KWF broken redirect page ${i}" "${text}"
  done

  for i in $(seq -w 1 10); do
    title="KWF Small ${i}"
    text="small-${i}"
    mw_edit "${title}" "Seed KWF small page ${i}" "${text}"
  done

  for i in $(seq -w 1 10); do
    title="KWF Large ${i}"
    text="KWF-LARGE-${i}: "
    text+=$(printf 'This sentence expands page size for filtering tests. %.0s' $(seq 1 80))
    mw_edit "${title}" "Seed KWF large page ${i}" "${text}"
  done

  # Deep category hierarchy dataset for recursive category search and
  # category-deletion ordering tests (leaf-to-root).
  mw_edit "Category:KWF Deep Root" \
    "Seed KWF deep category root" \
    "Root of a 5-level category hierarchy for Skubell testing."
  mw_edit "Category:KWF Deep L2 A" \
    "Seed KWF deep category L2A" \
    "Level 2 category A. [[Category:KWF Deep Root]]"
  mw_edit "Category:KWF Deep L2 B" \
    "Seed KWF deep category L2B" \
    "Level 2 category B. [[Category:KWF Deep Root]]"
  mw_edit "Category:KWF Deep L3 A1" \
    "Seed KWF deep category L3A1" \
    "Level 3 category A1. [[Category:KWF Deep L2 A]]"
  mw_edit "Category:KWF Deep L3 A2" \
    "Seed KWF deep category L3A2" \
    "Level 3 category A2. [[Category:KWF Deep L2 A]]"
  mw_edit "Category:KWF Deep L3 B1" \
    "Seed KWF deep category L3B1" \
    "Level 3 category B1. [[Category:KWF Deep L2 B]]"
  mw_edit "Category:KWF Deep L4 A1a" \
    "Seed KWF deep category L4A1a" \
    "Level 4 category A1a. [[Category:KWF Deep L3 A1]]"
  mw_edit "Category:KWF Deep L4 A1b" \
    "Seed KWF deep category L4A1b" \
    "Level 4 category A1b. [[Category:KWF Deep L3 A1]]"
  mw_edit "Category:KWF Deep L4 A2a" \
    "Seed KWF deep category L4A2a" \
    "Level 4 category A2a. [[Category:KWF Deep L3 A2]]"
  mw_edit "Category:KWF Deep L4 B1a" \
    "Seed KWF deep category L4B1a" \
    "Level 4 category B1a. [[Category:KWF Deep L3 B1]]"
  mw_edit "Category:KWF Deep L5 A1a i" \
    "Seed KWF deep category L5A1ai" \
    "Level 5 leaf category A1a-i. [[Category:KWF Deep L4 A1a]]"
  mw_edit "Category:KWF Deep L5 A1a ii" \
    "Seed KWF deep category L5A1aii" \
    "Level 5 leaf category A1a-ii. [[Category:KWF Deep L4 A1a]]"
  mw_edit "Category:KWF Deep L5 A1b i" \
    "Seed KWF deep category L5A1bi" \
    "Level 5 leaf category A1b-i. [[Category:KWF Deep L4 A1b]]"
  mw_edit "Category:KWF Deep L5 A2a i" \
    "Seed KWF deep category L5A2ai" \
    "Level 5 leaf category A2a-i. [[Category:KWF Deep L4 A2a]]"
  mw_edit "Category:KWF Deep L5 B1a i" \
    "Seed KWF deep category L5B1ai" \
    "Level 5 leaf category B1a-i. [[Category:KWF Deep L4 B1a]]"

  for i in $(seq -w 1 3); do
    mw_edit "KWF Deep Root Page ${i}" \
      "Seed KWF deep root member ${i}" \
      "Root-level category member ${i}. [[Category:KWF Deep Root]]"
    mw_edit "KWF Deep L2A Page ${i}" \
      "Seed KWF deep L2A member ${i}" \
      "L2A category member ${i}. [[Category:KWF Deep L2 A]]"
    mw_edit "KWF Deep L2B Page ${i}" \
      "Seed KWF deep L2B member ${i}" \
      "L2B category member ${i}. [[Category:KWF Deep L2 B]]"
    mw_edit "KWF Deep L3A1 Page ${i}" \
      "Seed KWF deep L3A1 member ${i}" \
      "L3A1 category member ${i}. [[Category:KWF Deep L3 A1]]"
    mw_edit "KWF Deep L3A2 Page ${i}" \
      "Seed KWF deep L3A2 member ${i}" \
      "L3A2 category member ${i}. [[Category:KWF Deep L3 A2]]"
    mw_edit "KWF Deep L3B1 Page ${i}" \
      "Seed KWF deep L3B1 member ${i}" \
      "L3B1 category member ${i}. [[Category:KWF Deep L3 B1]]"
    mw_edit "KWF Deep L4A1a Page ${i}" \
      "Seed KWF deep L4A1a member ${i}" \
      "L4A1a category member ${i}. [[Category:KWF Deep L4 A1a]]"
    mw_edit "KWF Deep L4A1b Page ${i}" \
      "Seed KWF deep L4A1b member ${i}" \
      "L4A1b category member ${i}. [[Category:KWF Deep L4 A1b]]"
    mw_edit "KWF Deep L4A2a Page ${i}" \
      "Seed KWF deep L4A2a member ${i}" \
      "L4A2a category member ${i}. [[Category:KWF Deep L4 A2a]]"
    mw_edit "KWF Deep L4B1a Page ${i}" \
      "Seed KWF deep L4B1a member ${i}" \
      "L4B1a category member ${i}. [[Category:KWF Deep L4 B1a]]"
    mw_edit "KWF Deep L5A1ai Page ${i}" \
      "Seed KWF deep L5A1ai member ${i}" \
      "L5A1ai category member ${i}. [[Category:KWF Deep L5 A1a i]]"
    mw_edit "KWF Deep L5A1aii Page ${i}" \
      "Seed KWF deep L5A1aii member ${i}" \
      "L5A1aii category member ${i}. [[Category:KWF Deep L5 A1a ii]]"
    mw_edit "KWF Deep L5A1bi Page ${i}" \
      "Seed KWF deep L5A1bi member ${i}" \
      "L5A1bi category member ${i}. [[Category:KWF Deep L5 A1b i]]"
    mw_edit "KWF Deep L5A2ai Page ${i}" \
      "Seed KWF deep L5A2ai member ${i}" \
      "L5A2ai category member ${i}. [[Category:KWF Deep L5 A2a i]]"
    mw_edit "KWF Deep L5B1ai Page ${i}" \
      "Seed KWF deep L5B1ai member ${i}" \
      "L5B1ai category member ${i}. [[Category:KWF Deep L5 B1a i]]"
  done

  # Separate category-loop dataset (if the wiki accepts it).
  mw_edit "Category:KWF Loop A" \
    "Seed KWF loop category A" \
    "Loop category A. [[Category:KWF Loop B]]"
  mw_edit "Category:KWF Loop B" \
    "Seed KWF loop category B" \
    "Loop category B. [[Category:KWF Loop A]]"
  for i in $(seq -w 1 3); do
    mw_edit "KWF Loop A Page ${i}" \
      "Seed KWF loop A member ${i}" \
      "Loop A member ${i}. [[Category:KWF Loop A]]"
    mw_edit "KWF Loop B Page ${i}" \
      "Seed KWF loop B member ${i}" \
      "Loop B member ${i}. [[Category:KWF Loop B]]"
  done
}

echo "Seeding deletion-workflow data on ${TARGET} via service ${APP_SERVICE}..."
"$(dirname "$0")/wait-for-wiki.sh" "${TARGET}"
seed_data

cat <<SUMMARY
Deletion-workflow dataset is ready on ${TARGET}
Created by regular user:
  - KWF Creator 01..12 authored by TestEditor
Criteria coverage:
  - Prefix: KWF PrefixAlpha 01..12
  - Category: Category:KWF Shared Category (20+ pages)
  - Creator: TestEditor (KWF Creator 01..12)
  - Linked from page: KWF Link Hub -> KWF Creator 01..10
  - Template transclusion: {{KWF Banner}} in KWF Creator 01..12
  - Namespace: Project:KWF Namespace 01..10
  - Redirects only: KWF Redirect Source 01..10
  - Broken redirects: KWF Broken Redirect 01..10
  - Size filter: KWF Small 01..10 and KWF Large 01..10
  - Deep categories (5 levels): Category:KWF Deep Root -> ... -> Category:KWF Deep L5*
    Each category has multiple pages; several categories contain multiple subcategories.
  - Category loop test: Category:KWF Loop A <-> Category:KWF Loop B
SUMMARY
