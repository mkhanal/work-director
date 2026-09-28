# Projects

## A New Project File Carries No Lazyspec Claim
`projectTemplate` renders a project file with concrete frontmatter and no lazyspec field or verdict; lazyspec presence is the repo's own fact, installed when the project confirms it.

## Confirming The Preferred Lazyspec Files An Install Work Item
`installLazyspec` returns an evolution work item whose detail says to install the director's preferred lazyspec in the project's own agent files.

## A New Project Defaults To Auto Mode And Records A Chosen Model
`projectTemplate` defaults mode to `auto` and, when a model is given, writes it into frontmatter; `parseProject` honours both, defaulting a missing mode to `auto` and treating a blank model as none.