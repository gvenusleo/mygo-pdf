#let ink = rgb("#162b43")
#let muted = rgb("#465d74")
#let accent = rgb("#1a61a4")

#set page(
  paper: "a5",
  margin: (x: 17mm, top: 22mm, bottom: 17mm),
  footer: align(right, text(size: 9pt, fill: muted, context counter(page).display("1"))),
)
#set text(font: "Libertinus Serif", size: 13pt, fill: ink)
#set par(leading: 0.68em)
#show heading.where(level: 1): set text(size: 31pt, weight: "bold")
#show link: set text(fill: accent)

= MyGo PDF

#text(size: 16pt, fill: muted)[A native viewer for desktop apps]
#v(6mm)
#line(length: 100%, stroke: 0.65pt + rgb("#bdcbd9"))
#v(5mm)

Embed a PDF in a MyGo window to read a local document or preview a file your app generates.

#v(5mm)
*Navigate the document* \
Enter a page number or pick a bookmark in the outline. Use the View menu to fit the page, fit the width or rotate the document.

#v(5mm)
*Find and copy text* \
Open search and look for “selection”. You can also drag over this sentence and copy it.

#v(6mm)
#link(<second>)[Continue to links and text →]

#pagebreak()

= Links and text <second>

*Text selection* \
The viewer reads the PDF's text layer. It follows document permissions for copying and assistive access.

#v(5mm)
*Links* \
PDF links can point to another page or to a web address. Try visiting the #link("https://github.com/egoist/mygo")[MyGo project] or return to page one with the toolbar.

#v(5mm)
*Live preview* \
When a host refreshes a document, the viewer keeps the reading position and shows the old image until the new visible pages are ready.
