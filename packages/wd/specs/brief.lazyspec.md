# Brief

## A Brief Carries Only Cards Whose Scope Matches The Project
Adopted cards with scope `project:<name>` or `lang:`/`stack:` matching the project's stack appear; global cards do not, because the taste plugin already delivers them.

## A Brief Names The Project Workflows And Verify Commands
The brief lists the project's workflows and verify commands, and the repo's instructions file.

## Human Hour Estimates Are Rejected From Briefs
A title or detail containing an hour, story-point or person-day estimate throws HumanHoursRejected quoting the match.

## A Brief Ends With The Report Format
The brief ends with the STATUS/FILES/VERIFY/PR/NOTES report format.

## A Brief Carries Context And A Default For Ambiguity
The brief includes the project roadmap, the decisions already made, relevant history, and tells the executor to decide ambiguities itself and report at plan and done.

## A Task Brief Names Its Epic And Co-Workers' Claims
A task's brief carries the epic goal, its heading, the other executors' active claims (who owns which paths) and the `wd` coordination contract, and carries no roadmap or history.

## An Epic Brief Lists The Open Tasks Grouped By Heading
An epic's spawn brief carries the compact open task list grouped by heading and names how to claim tasks.

## A Brief Does Not Claim Lazyspec
The brief carries no verdict on lazyspec. Whether the project uses it is the repo's own fact: if the repo installed lazyspec, its agent files and listed workflows (`/lazyspec`) make the executor honor it; the director names those and nothing more.
