# Encrypted PDF test fixtures

Both fixtures are generated from the [sample PDF in v0.3.1](https://github.com/gvenusleo/mygo-pdf/blob/v0.3.1/examples/viewer/sample.pdf). They share the user password `café` and owner password `fixture-owner`. The [password and permissions test](../reading_test.go) uses them to check password retries, copying and assistive text extraction.

| Fixture | Ordinary copying | Assistive text extraction |
| --- | --- | --- |
| `restricted.pdf` | Restricted | Allowed |
| `inaccessible.pdf` | Restricted | Restricted |

## Regenerate the fixtures

With `pypdf==7.0.0` installed and the `v0.3.1` tag available locally, run the following Python snippet from the repository root.

```python
from io import BytesIO
from subprocess import check_output

from pypdf import PdfWriter
from pypdf.constants import UserAccessPermissions as P

sample = check_output(["git", "show", "v0.3.1:examples/viewer/sample.pdf"])

for filename, permissions in (
    ("restricted.pdf", P(0xfffffffc) & ~P.EXTRACT),
    ("inaccessible.pdf", P(0xfffffffc) & ~(P.EXTRACT | P.EXTRACT_TEXT_AND_GRAPHICS)),
):
    writer = PdfWriter(clone_from=BytesIO(sample))
    writer.encrypt("café", "fixture-owner", permissions_flag=permissions, algorithm="RC4-128")
    with open("pdf/testdata/" + filename, "wb") as output:
        writer.write(output)
```
