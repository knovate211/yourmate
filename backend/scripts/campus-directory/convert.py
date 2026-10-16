#!/usr/bin/env python3
"""Converts the campus master list spreadsheet into the CSV the identity
service imports at startup (services/identity/data/campus_directory.csv).

    python3 -I scripts/campus-directory/convert.py college_university_master_list_500.xlsx \
        > services/identity/data/campus_directory.csv

Standard library only. Expected columns: No., Institution Name, Country / Region,
City / State, Official Website, Public Contact Email, Verification Status.

Email domains are deliberately not imported: a campus domain auto-verifies
students, so it must be confirmed by an admin in the app before it is trusted.
"""
import csv
import re
import sys
import xml.etree.ElementTree as ET
import zipfile

NS = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}

# Country/region as written in the list -> (country, city, timezone, currency).
# The list's "City / State" column only repeats the country for India and
# Germany, so no city is recorded for those.
REGIONS = {
    "India": ("India", "", "Asia/Kolkata", "INR"),
    "Germany": ("Germany", "", "Europe/Berlin", "EUR"),
    "London": ("United Kingdom", "London", "Europe/London", "GBP"),
}


def read_rows(path):
    z = zipfile.ZipFile(path)
    shared = []
    if "xl/sharedStrings.xml" in z.namelist():
        for si in ET.fromstring(z.read("xl/sharedStrings.xml")).findall("m:si", NS):
            shared.append("".join(t.text or "" for t in si.iter("{%s}t" % NS["m"])))
    sheet = ET.fromstring(z.read("xl/worksheets/sheet1.xml"))
    for row in sheet.iter("{%s}row" % NS["m"]):
        vals = {}
        for c in row.findall("m:c", NS):
            col = re.match(r"[A-Z]+", c.get("r")).group()
            v = c.find("m:v", NS)
            if c.get("t") == "s" and v is not None:
                vals[col] = shared[int(v.text)]
            elif c.get("t") == "inlineStr":
                vals[col] = "".join(x.text or "" for x in c.iter("{%s}t" % NS["m"]))
            else:
                vals[col] = v.text if v is not None else ""
        yield vals


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    rows = list(read_rows(sys.argv[1]))
    header, body = rows[0], rows[1:]
    if header.get("B") != "Institution Name" or header.get("C") != "Country / Region":
        sys.exit(f"unexpected header: {header}")

    out = csv.writer(sys.stdout, lineterminator="\n")
    out.writerow(["list_no", "name", "country", "city", "timezone", "currency", "website"])
    seen = set()
    for r in body:
        name = " ".join((r.get("B") or "").split())
        region = (r.get("C") or "").strip()
        if not name:
            continue
        if region not in REGIONS:
            sys.exit(f"row {r.get('A')}: unknown country/region {region!r}; add it to REGIONS")
        if name.lower() in seen:
            print(f"skipping duplicate: {name}", file=sys.stderr)
            continue
        seen.add(name.lower())
        country, city, tz, currency = REGIONS[region]
        out.writerow([r.get("A", ""), name, country, city, tz, currency, (r.get("E") or "").strip()])
    print(f"{len(seen)} campuses", file=sys.stderr)


if __name__ == "__main__":
    main()
