# Postman

Two files:

- `Hyperwake.postman_collection.json` — 30 requests in four folders
- `Hyperwake.local.postman_environment.json` — `baseUrl`, and an empty `token` for you to paste into

## Use

1. Start the engine: `npx hyperwake` (or `node bin/hyperwake.js start`). It
   prints an API URL and a token.
2. Import both files into Postman and paste the printed token into the
   environment's `token` variable. It ships empty on purpose: a token committed
   to a repository is a token you have to rotate.
3. Open the **Walkthrough** folder and press **Run**.

It creates a fresh Omarchy machine, polls until the guest reports ready, runs a
shell command, round-trips a file, takes a screenshot, mints a desktop URL, then
stops, deletes and checks nothing was left behind. Sixteen assertions, about
forty seconds.

**Step 8 prints a desktop URL to the Postman console. Open it in a browser** to
see the live desktop.

## From the command line

```sh
npx newman run postman/Hyperwake.postman_collection.json \
  -e postman/Hyperwake.local.postman_environment.json \
  --folder Walkthrough --timeout-request 200000 --delay-request 2500
```

## The other folders

- **Machines** — lifecycle endpoints on their own, for poking at by hand
- **Actions** — one request per verb: exec, read_file, write_file, screenshot,
  click, move, scroll, type, key
- **Health** — the unauthenticated health check

`Create machine` in either folder writes the new id into `{{machineId}}`, so the
rest work without editing anything.

## Note on variables

An active environment shadows collection variables in Postman, so the scripts
write `machineId` to both. If you run without an environment, the collection
variable is used. Either way it just works — this is recorded because the first
version of the collection got it wrong and every request after `create` 404'd.
