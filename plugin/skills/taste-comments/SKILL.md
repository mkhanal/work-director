---
name: taste-comments
description: Use when about to write a comment.
---
# Taste: comments

## Comments state constraints, not narration
A comment states an invariant the code cannot express. Never narrate the change, the history or the reviewer.
**Why:** Narration ages into lies; a renamed identifier beats a comment describing it.
**Apply:** Allowed: why something counterintuitive is intentional, an external constraint, tech debt with a ticket.
