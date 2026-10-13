//! What the app puts on the clipboard when something is copied.

use crate::ledger::Work;
use crate::markdown::{self, Block};

/// Markdown as it reads: its words, without the marks that style them.
pub fn plain(source: &str) -> String {
    blocks(markdown::parse(source))
}

fn blocks(blocks: Vec<Block>) -> String {
    blocks
        .into_iter()
        .filter_map(|block| match block {
            Block::Heading(_, title) => Some(words(&title)),
            Block::Bullet { text, number, depth } => {
                Some(format!("{}{} {}", "  ".repeat(depth), number.as_deref().unwrap_or("•"), words(&text)))
            }
            Block::Code(code) => Some(code),
            Block::Paragraph(paragraph) => Some(words(&paragraph)),
            Block::Table(table) => Some(table_rows(&table)),
            Block::Quote(inner) => Some(self::blocks(inner)),
            Block::Rule => None,
        })
        .collect::<Vec<_>>()
        .join("\n\n")
}

/// A table as copied: a line per row, header first, its cells' words
/// separated by tabs.
pub fn table_rows(table: &markdown::Table) -> String {
    std::iter::once(&table.head)
        .chain(&table.rows)
        .map(|row| row.iter().map(|cell| words(cell)).collect::<Vec<_>>().join("\t"))
        .collect::<Vec<_>>()
        .join("\n")
}

/// Inline markdown's words: its markup rendered, then the tags dropped.
pub fn words(text: &str) -> String {
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
