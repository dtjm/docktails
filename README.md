docktails (awoo-oo!)
====================

A simple program for viewing logs from your Docker containers.  When you run it,
it starts outputting logs from all running containers, and watches Docker events
for new containers that get started, and will output those logs as well.

Installation
------------

```
go get -u -v github.com/dtjm/docktails
```

Usage
-----

```
docktails

Usage of docktails:
  -json=true: Pretty-print JSON
```

