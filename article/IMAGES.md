# Article images

Four images: one licensed photo header and three screenshots from the live site. The screenshots were captured November 16, 2026 at 1280px viewport and cropped to the card/stats region. All files are under 220 KB each (LinkedIn limit is 5 MB).

The files live once, in `assets/` at the repo root, and are copied flat into the published site by the build. The article markdown references them by relative path (`../assets/...`) so they render on the branch, in the PR preview, and in the merged repo folder view.

| File (repo path) | Pages URL after build | Where it goes in the article |
| --- | --- | --- |
| `assets/article-header-boston.jpg` | https://todkap.github.io/permanent-rotation/article-header-boston.jpg | Top of the article, directly under the title. The Boston skyline from the Longfellow Bridge at golden hour. Licensed photo, see credit below. |
| `assets/article-song-card.png` | https://todkap.github.io/permanent-rotation/article-song-card.png | After the "final round made the site itself more visual" paragraph in What the tool does. The Mayonaise card: cover, memory, AI note with source, seen-live tag. |
| `assets/article-stats.png` | https://todkap.github.io/permanent-rotation/article-stats.png | Directly below the song card image. The stats page header: 50 songs, 5 eras, 1971 to 2020, 25 seen live. |
| `assets/article-deep-cover.png` | https://todkap.github.io/permanent-rotation/article-deep-cover.png | End of Where memory beats the data, after the Deep Cover paragraph. The unmatched-song case with its placeholder cover. Optional on LinkedIn if the article feels long. |

## Header photo credit (required)

`article-header-boston.jpg` is "Boston skyline from Longfellow Bridge September 2017 panorama 2" by King of Hearts, CC BY-SA 4.0, via Wikimedia Commons: https://commons.wikimedia.org/wiki/File:Boston_skyline_from_Longfellow_Bridge_September_2017_panorama_2.jpg

The credit line is already in the markdown directly under the header image:

*Photo: King of Hearts, CC BY-SA 4.0, via Wikimedia Commons*

CC BY-SA 4.0 requires that credit wherever the image appears, and the cropped version stays under CC BY-SA 4.0. On LinkedIn, add the same line either directly under the header image or in one line at the end of the article; either placement satisfies the license as long as it includes the author, the license, and the source link.

## For the LinkedIn upload

LinkedIn articles do not hotlink reliably during editing; upload each image file at its placement point instead. The header goes in as the article's hero image at the top (it is already cropped to LinkedIn's 1200x627 hero size), the other three go inline where the table above says.

## Refresh note (Friday)

The three screenshots were captured before the pending smarter-matching PR and the next notes run. That PR changes the displayed card year on 4 songs. If any of the affected cards appear in these shots, or if you want pixel-fresh screenshots after everything merges, recapture the same three regions after the final build: one song card on the timeline, the stats header, the Deep Cover card. Crop boxes used: song card (195,60)-(1090,432), Deep Cover (195,60)-(1090,420), stats (180,130)-(1100,545) on a 1280x720 viewport. The header photo does not change.
