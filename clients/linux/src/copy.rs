//! What the app puts on the clipboard when something is copied.

use crate::ledger::Work;
use crate::markdown::{self, Block};

/// Markdown as it reads: its words, without the marks that style them.
pub fn plain(source: &str) -> String {
    markdown::parse(source)
        .into_iter()
        .map(|block| match block {
            Block::Heading(_, title) => words(&title),
            Block::Bullet(item, number) => format!("{} {}", number.as_deref().unwrap_or("•"), words(&item)),
            Block::Code(code) => code,
            Block::Paragraph(paragraph) => words(&paragraph),
        })
        .collect::<Vec<_>>()
        .join("\n\n")
}

/// Inline markdown's words: its markup rendered, then the tags dropped.
fn words(text: &str) -> String {
    let markup = markdown::inline(text);
    let mut out = String::with_capacity(markup.len());
    let mut in_tag = false;
    for c in markup.chars() {
        match c {
            '<' => in_tag = true,
            '>' if in_tag => in_tag = false,
            _ if !in_tag => out.push(c),
            _ => {}
        }
    }
    out.replace("&lt;", "<").replace("&gt;", ">").replace("&quot;", "\"").replace("&#39;", "'").replace("&amp;", "&")
}

/// Work as one line: its title and its id.
pub fn work(w: &Work) -> String {
    format!("{} ({})", w.title, w.id)
}
