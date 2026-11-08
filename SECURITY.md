# Security policy

## Supported versions

ant has not reached 1.0. Until it does, only the latest release gets security fixes. From 1.0, fixes go to the latest minor release of the current major version.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Anything older | No |

## Reporting a vulnerability

Please don't open a public issue. Report it privately through GitHub instead:

1. Go to [Report a vulnerability](https://github.com/skudra-go/ant/security/advisories/new) (the **Security** tab, then **Report a vulnerability**).
2. Describe the problem, the versions it affects, and how to reproduce it.

You should hear back within a week. Once a fix is released, we publish an advisory and credit you, unless you'd rather we didn't.

## Using ant safely

Values are always bound as parameters, and column names come from struct fields. The raw SQL APIs (`ant.Raw`, `ant.SQL`, `ant.FromRaw` and friends) are the exception: never build their SQL from user input. The [security reference](https://github.com/skudra-go/ant-docs/blob/main/docs/reference/security.mdx) covers this and the known limits.
