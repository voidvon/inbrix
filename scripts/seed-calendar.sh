#!/usr/bin/env bash
# Seed a Radicale calendar with a plausible working week.
#
# Dates are pinned relative to a fixed anchor rather than "today" so a capture
# taken next month frames the same week — the screenshot set is meant to
# reproduce, not to drift with the clock.
set -euo pipefail

CAL="${1:-http://127.0.0.1:5232/demo/work}"
# Monday of the anchor week.
MON="${2:-20260803}"

d() { python3 -c "
import datetime,sys
base=datetime.datetime.strptime('$MON','%Y%m%d')
print((base+datetime.timedelta(days=int(sys.argv[1]))).strftime('%Y%m%d'))" "$1"; }

ev() {  # uid  day-offset  start  end  summary  location
  local uid="$1" day="$2" st="$3" en="$4" sum="$5" loc="${6:-}"
  local date; date="$(d "$day")"
  curl -s -o /dev/null -w "  %{http_code}  $sum\n" \
    -X PUT "$CAL/$uid.ics" -u demo:demo \
    -H "Content-Type: text/calendar; charset=utf-8" \
    --data-binary "BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//lilmail//demo seed//EN
BEGIN:VEVENT
UID:$uid@demo.lilmail.dev
DTSTAMP:${date}T090000Z
DTSTART:${date}T${st}00Z
DTEND:${date}T${en}00Z
SUMMARY:$sum
LOCATION:$loc
END:VEVENT
END:VCALENDAR"
}

ev standup-mon   0 0900 0915 "Standup"                    "Meet"
ev roadmap       0 1400 1500 "Product roadmap Q3"         "Meet"
ev standup-tue   1 0900 0915 "Standup"                    "Meet"
ev design-review 1 1100 1200 "Design review — landing"    "Studio"
ev onec          1 1600 1630 "1:1 with Alice"             "Meet"
ev standup-wed   2 0900 0915 "Standup"                    "Meet"
ev imap-debug    2 1300 1430 "IMAP IDLE reconnect debug"  "ENG-419"
ev standup-thu   3 0900 0915 "Standup"                    "Meet"
ev release       3 1500 1600 "Release cut — v1.14.0"      "Meet"
ev standup-fri   4 0900 0915 "Standup"                    "Meet"
ev retro         4 1530 1630 "Retro"                      "Meet"
