//! Markdown as an executor writes it: headings, lists, code blocks, tables,
//! quotes, rules and paragraphs, each with its inline emphasis, code and
//! links, made into the Pango markup a label renders.

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum Block {
    Heading(usize, String),
    /// A list item: its number when the list is ordered, and how many lists
    /// it sits inside.
    Bullet {
        text: String,
        number: Option<String>,
        depth: usize,
    },
    Code(String),
    Paragraph(String),
    Table(Table),
    Quote(Vec<Block>),
    Rule,
}

/// A pipe table: its header, each column's alignment, and its rows, every
/// row as wide as the header. Cells hold their inline markdown.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Table {
    pub head: Vec<String>,
    pub align: Vec<Align>,
    pub rows: Vec<Vec<String>>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Align {
    Start,
    Center,
    End,
}

pub fn parse(text: &str) -> Vec<Block> {
    let lines: Vec<&str> = text.split('\n').collect();
    let mut out = Vec::new();
    let mut paragraph: Vec<&str> = Vec::new();
    let mut code: Option<Vec<&str>> = None;
    // The indent of each list the next item may sit inside.
    let mut lists: Vec<usize> = Vec::new();
    let flush = |paragraph: &mut Vec<&str>, out: &mut Vec<Block>| {
        if !paragraph.is_empty() {
            out.push(Block::Paragraph(paragraph.join("\n")));
            paragraph.clear();
        }
    };
    let mut i = 0;
    while i < lines.len() {
        let raw = lines[i];
        let line = raw.trim();
        i += 1;
        if line.starts_with("```") {
            match code.take() {
                Some(open) => out.push(Block::Code(open.join("\n"))),
                None => {
                    flush(&mut paragraph, &mut out);
                    lists.clear();
                    code = Some(Vec::new());
                }
            }
            continue;
        }
        if let Some(open) = code.as_mut() {
            open.push(raw);
            continue;
        }
        let hashes = line.chars().take_while(|c| *c == '#').count();
        let item = line.strip_prefix("- ").or_else(|| line.strip_prefix("* ")).or_else(|| line.strip_prefix("+ "));
        if line.is_empty() {
            flush(&mut paragraph, &mut out);
        } else if let Some((head, align)) = lines.get(i).and_then(|next| table_head(line, next)) {
            flush(&mut paragraph, &mut out);
            lists.clear();
            let mut rows = Vec::new();
            i += 1;
            while let Some(row) = lines.get(i).map(|l| l.trim()).filter(|l| l.contains('|')) {
                let mut cells = cells(row);
                cells.resize(head.len(), String::new());
                rows.push(cells);
                i += 1;
            }
            out.push(Block::Table(Table { head, align, rows }));
        } else if let Some(quoted) = line.strip_prefix('>') {
            flush(&mut paragraph, &mut out);
            lists.clear();
            let mut inner = vec![quoted.strip_prefix(' ').unwrap_or(quoted)];
            while let Some(more) = lines.get(i).and_then(|l| l.trim().strip_prefix('>')) {
                inner.push(more.strip_prefix(' ').unwrap_or(more));
                i += 1;
            }
            out.push(Block::Quote(parse(&inner.join("\n"))));
        } else if rule(line) {
            flush(&mut paragraph, &mut out);
            lists.clear();
            out.push(Block::Rule);
        } else if hashes > 0 && line[hashes..].starts_with(' ') {
            flush(&mut paragraph, &mut out);
            lists.clear();
            out.push(Block::Heading(hashes, line[hashes..].trim().to_string()));
        } else if let Some(item) = item {
            flush(&mut paragraph, &mut out);
            let depth = depth(&mut lists, raw);
            out.push(Block::Bullet { text: item.to_string(), number: None, depth });
        } else if let Some((number, item)) = ordered(line) {
            flush(&mut paragraph, &mut out);
            let depth = depth(&mut lists, raw);
            out.push(Block::Bullet { text: item.to_string(), number: Some(number.to_string()), depth });
        } else {
            lists.clear();
            paragraph.push(raw);
        }
    }
    if let Some(open) = code {
        out.push(Block::Code(open.join("\n")));
    }
    flush(&mut paragraph, &mut out);
    out
}

/// How many lists an item indented as `raw` is nested in: those of the
/// items before it that are indented less.
fn depth(lists: &mut Vec<usize>, raw: &str) -> usize {
    let indent = raw.chars().take_while(|c| c.is_whitespace()).map(|c| if c == '\t' { 4 } else { 1 }).sum();
    while lists.last().is_some_and(|open| *open >= indent) {
        lists.pop();
    }
    lists.push(indent);
    lists.len() - 1
}

/// `---`, `***` or `___`: three or more of one mark, spaces allowed between.
fn rule(line: &str) -> bool {
    let mut marks = line.chars().filter(|c| !c.is_whitespace());
    let Some(first) = marks.next() else { return false };
    matches!(first, '-' | '*' | '_') && line.chars().filter(|c| *c == first).count() >= 3 && marks.all(|c| c == first)
}

/// A table's header and alignments, when `line` is a row of cells and
/// `next` is the separator row under it, with a cell for each.
fn table_head(line: &str, next: &str) -> Option<(Vec<String>, Vec<Align>)> {
    if !line.contains('|') || !next.contains('-') {
        return None;
    }
    let head = cells(line);
    let align = cells(next)
        .iter()
        .map(|s| {
            let dashes = s.trim_matches(':');
            (!dashes.is_empty() && dashes.chars().all(|c| c == '-')).then_some(
                match (s.starts_with(':'), s.ends_with(':')) {
                    (true, true) => Align::Center,
                    (false, true) => Align::End,
                    _ => Align::Start,
                },
            )
        })
        .collect::<Option<Vec<_>>>()?;
    (align.len() == head.len()).then_some((head, align))
}

/// A row's cells, trimmed, split at each pipe not escaped with `\`; the pipes
/// at either end are optional.
fn cells(row: &str) -> Vec<String> {
    let row = row.trim();
    let row = row.strip_prefix('|').unwrap_or(row);
    let row = match row.strip_suffix('|') {
        Some(inner) if !inner.ends_with('\\') => inner,
        _ => row,
    };
    let mut out = Vec::new();
    let mut cell = String::new();
    let mut chars = row.chars().peekable();
    while let Some(c) = chars.next() {
        match c {
            '\\' if chars.peek() == Some(&'|') => {
                cell.push('|');
                chars.next();
            }
            '|' => out.push(std::mem::take(&mut cell).trim().to_string()),
            _ => cell.push(c),
        }
    }
    out.push(cell.trim().to_string());
    out
}

/// "12. item" as ("12.", "item").
fn ordered(line: &str) -> Option<(&str, &str)> {
    let dot = line.find('.')?;
    let digits = &line[..dot];
    if digits.is_empty() || !digits.chars().all(|c| c.is_ascii_digit()) {
        return None;
    }
    let item = line[dot + 1..].strip_prefix(' ')?;
    Some((&line[..=dot], item))
}

/// Text with Pango's special characters escaped.
pub fn escape(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for c in text.chars() {
        match c {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&#39;"),
            _ => out.push(c),
        }
    }
    out
}

/// Inline markdown as Pango markup: `code`, **bold**, *italic*, _italic_ and
/// [links](url). Every tag opened is closed, so the markup always parses.
pub fn inline(text: &str) -> String {
    let mut out = String::new();
    for run in runs(text) {
        let mut open = String::new();
        let mut close = String::new();
        if let Some(url) = &run.link {
            open.push_str(&format!("<a href=\"{}\">", escape(url)));
            close.insert_str(0, "</a>");
        }
        if run.code {
            open.push_str("<span font_family=\"monospace\" background=\"#8080801f\">");
            close.insert_str(0, "</span>");
        }
        if run.bold {
            open.push_str("<b>");
            close.insert_str(0, "</b>");
        }
        if run.italic {
            open.push_str("<i>");
            close.insert_str(0, "</i>");
        }
        out.push_str(&open);
        out.push_str(&escape(&run.text));
        out.push_str(&close);
    }
    out
}

/// A stretch of inline markdown's words styled one way.
#[derive(Debug, Clone, Default, PartialEq, Eq, Hash)]
pub struct Run {
    pub text: String,
    pub bold: bool,
    pub italic: bool,
    pub code: bool,
    pub link: Option<String>,
}

/// Inline markdown as its words, in runs styled one way each: `code`,
/// **bold**, *italic*, _italic_ and [links](url), the marks dropped.
pub fn runs(text: &str) -> Vec<Run> {
    let mut out: Vec<Run> = Vec::new();
    styled(text, &Run::default(), &mut out);
    out
}

fn styled(text: &str, style: &Run, out: &mut Vec<Run>) {
    let push = |out: &mut Vec<Run>, run: Run| match out.last_mut() {
        Some(last) if Run { text: String::new(), ..last.clone() } == Run { text: String::new(), ..run.clone() } => {
            last.text.push_str(&run.text)
        }
        _ => out.push(run),
    };
    let chars: Vec<char> = text.chars().collect();
    let mut i = 0;
    while i < chars.len() {
        let c = chars[i];
        if c == '`' {
            if let Some(end) = find(&chars, i + 1, &['`']) {
                let inner: String = chars[i + 1..end].iter().collect();
                push(out, Run { text: inner, code: true, ..style.clone() });
                i = end + 1;
                continue;
            }
        }
        if c == '*' && chars.get(i + 1) == Some(&'*') {
            if let Some(end) = find(&chars, i + 2, &['*', '*']) {
                if end > i + 2 {
                    let inner: String = chars[i + 2..end].iter().collect();
                    styled(&inner, &Run { bold: true, ..style.clone() }, out);
                    i = end + 2;
                    continue;
                }
            }
        }
        if (c == '*' || c == '_') && opens_emphasis(&chars, i) {
            if let Some(end) = find(&chars, i + 1, &[c]) {
                let closes = !chars[end - 1].is_whitespace()
                    && (c == '*' || chars.get(end + 1).is_none_or(|n| !n.is_alphanumeric()));
                if end > i + 1 && closes {
                    let inner: String = chars[i + 1..end].iter().collect();
                    styled(&inner, &Run { italic: true, ..style.clone() }, out);
                    i = end + 1;
                    continue;
                }
            }
        }
        if c == '[' {
            if let Some(close) = find(&chars, i + 1, &[']', '(']) {
                if let Some(end) = find(&chars, close + 2, &[')']) {
                    let label: String = chars[i + 1..close].iter().collect();
                    let url: String = chars[close + 2..end].iter().collect();
                    push(out, Run { text: label, link: Some(url), ..style.clone() });
                    i = end + 1;
                    continue;
                }
            }
        }
        push(out, Run { text: c.to_string(), ..style.clone() });
        i += 1;
    }
}

/// An emphasis marker opens one when a word follows it and, for `_`, when it
/// does not sit inside a word (snake_case stays as written).
fn opens_emphasis(chars: &[char], i: usize) -> bool {
    let next_is_word = chars.get(i + 1).is_some_and(|n| !n.is_whitespace() && *n != chars[i]);
    let inside_word = i > 0 && chars[i - 1].is_alphanumeric();
    next_is_word && (chars[i] == '*' || !inside_word)
}

/// The index where `pattern` next starts at or after `from`.
fn find(chars: &[char], from: usize, pattern: &[char]) -> Option<usize> {
    if from + pattern.len() > chars.len() {
        return None;
    }
    (from..=chars.len() - pattern.len()).find(|&i| chars[i..i + pattern.len()] == *pattern)
}
