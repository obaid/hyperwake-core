# Contributing

Start with the README and `docs/development.md`. The public product is a Computer;
provider details stay behind the compute driver. A roadmap or prototype is not
proof that an integration works.

## Run checks

```sh
php artisan test
npm run types:check
npm run check
vendor/bin/phpstan analyse
(cd guest/hyperwake-guest && go test ./...)
python3 -m unittest discover -s tests/Python -v
```

Use `vendor/bin/pint` for PHP formatting. For the self-hosted package also build
with `bin/build-images` and run the acceptance test on Linux x86_64 with KVM.
Never run `migrate:fresh` against a shared development database.

## Changes we welcome

Improve installation errors, docs, agent workflows, runtime reliability, and
coverage for ownership/authorization boundaries. Include the user-visible result
and the evidence you tested. Explain any untested platform assumptions explicitly.

Do not commit tokens, personal environments, guest disks, research outputs, or
screenshots containing credentials. The original code is MIT licensed; preserve
third-party attribution and license notices when importing code.

A public release needs actual Omarchy desktop evidence. Passing a Debian desktop
fixture test does not satisfy that gate. Avoid adding large dependencies or new
infrastructure layers without an issue explaining the customer need.
