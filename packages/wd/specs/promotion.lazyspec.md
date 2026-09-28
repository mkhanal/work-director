# Promotion scan

## Only Adopted Project Cards With Recurring Evidence Graduate
A promotion candidate exists only for an adopted card with a `project:` scope whose feedback is
from ≥2 different projects, or has ≥2 `attached` feedback. Cards without that evidence do not qualify.

## Adopting A Candidate Writes A Global Candidate Card
`adoptCard` writes the card's file under `cards/<category>/<id>.md` with scope `[global]`,
status `candidate`, empty enforce, and the evidence ids — never a direct promotion to adopted.