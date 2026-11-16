# Article images

Three screenshots from the live site, captured November 16, 2026 at 1280px viewport and cropped to the card/stats region. All PNG, under 100 KB each (LinkedIn limit is 5 MB; ~1200px wide recommended, these are 895-920px wide and render fine in the article body).

The files live once, in `assets/` at the repo root, and are copied flat into the published site by the build. The article markdown references them by absolute Pages URL so the same markdown works on GitHub and when pasted into LinkedIn.

| File (repo path) | Pages URL after build | Where it goes in the article |
| --- | --- | --- |
| `assets/article-song-card.png` | https://todkap.github.io/permanent-rotation/article-song-card.png | After the "final round made the site itself more visual" paragraph in What the tool does. The Mayonaise card: cover, memory, AI note with source, seen-live tag. |
| `assets/article-stats.png` | https://todkap.github.io/permanent-rotation/article-stats.png | Directly below the song card image. The stats page header: 50 songs, 5 eras, 1971 to 2020, 25 seen live. |
| `assets/article-deep-cover.png` | https://todkap.github.io/permanent-rotation/article-deep-cover.png | End of Where memory beats the data, after the Deep Cover paragraph. The unmatched-song case with its placeholder cover. Optional on LinkedIn if the article feels long. |

## For the LinkedIn upload

LinkedIn articles do not hotlink reliably during editing; upload each PNG at its placement point instead. Order and placement match the table above.

## Refresh note (Friday)

These shots were captured before the pending smarter-matching PR and the next notes run. That PR changes the displayed card year on 4 songs. If any of the affected cards appear in these shots, or if you want pixel-fresh screenshots after everything merges, recapture the same three regions after the final build: one song card on the timeline, the stats header, the Deep Cover card. Crop boxes used: song card (195,60)-(1090,432), Deep Cover (195,60)-(1090,420), stats (180,130)-(1100,545) on a 1280x720 viewport.
