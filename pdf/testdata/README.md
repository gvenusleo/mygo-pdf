# Encrypted PDF test fixtures

The fixtures use the repository's sample PDF. Both use the user password `café` and owner password `fixture-owner`. `restricted.pdf` allows assistive text extraction and restricts ordinary copying. `inaccessible.pdf` restricts both.

From the repository root, run the following with pypdf 7.0.0 to regenerate both fixtures.

```python
from pypdf import PdfWriter
from pypdf.constants import UserAccessPermissions as P

for filename, permissions in (
    ("restricted.pdf", P(0xfffffffc) & ~P.EXTRACT),
    ("inaccessible.pdf", P(0xfffffffc) & ~(P.EXTRACT | P.EXTRACT_TEXT_AND_GRAPHICS)),
):
    writer = PdfWriter(clone_from="examples/viewer/sample.pdf")
    writer.encrypt("café", "fixture-owner", permissions_flag=permissions, algorithm="RC4-128")
    with open("pdf/testdata/" + filename, "wb") as output:
        writer.write(output)
```
