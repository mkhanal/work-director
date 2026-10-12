//! Markdown as an executor writes it: headings, lists, code blocks and
//! paragraphs, each with its inline emphasis, code and links, made into the
//! Pango markup a label renders.

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Block {
    Heading(usize, String),
    /// A list item, with its number when the list is ordered.
    Bullet(String, Option<String>),
    Code(String),
    Paragraph(String),
}

pub fn parse(text: &str) -> Vec<Block> {
    let mut out = Vec::new();
    let mut paragraph: Vec<&str> = Vec::new();
    let mut code: Option<Vec<&str>> = None;
    let flush = |paragraph: &mut Vec<&str>, out: &mut Vec<Block>| {
        if !paragraph.is_empty() {
            out.push(Block::Paragraph(paragraph.join("\n")));
            paragraph.clear();
        }
    };
    for raw in text.split('\n') {
        let line = raw.trim();
        if line.starts_with("```") {
            match code.take() {
                Some(open) => out.push(Block::Code(open.join("\n"))),
                None => {
                    flush(&mut paragraph, &mut out);
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
        if line.is_empty() {
            flush(&mut paragraph, &mut out);
        } else if hashes > 0 && line[hashes..].starts_with(' ') {
            flush(&mut paragraph, &mut out);
            out.push(Block::Heading(hashes, line[hashes..].trim().to_string()));
        } else if let Some(item) = line.strip_prefix("- ").or_else(|| line.strip_prefix("* ")) {
            flush(&mut paragraph, &mut out);
            out.push(Block::Bullet(item.to_string(), None));
        } else if let Some((number, item)) = ordered(line) {
            flush(&mut paragraph, &mut out);
            out.push(Block::Bullet(item.to_string(), Some(number.to_string())));
        } else {
            paragraph.push(raw);
        }
    }
    if let Some(open) = code {
        out.push(Block::Code(open.join("\n")));
    }
    flush(&mut paragraph, &mut out);
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
    let chars: Vec<char> = text.chars().collect();
    let mut out = String::new();
    let mut i = 0;
    while i < chars.len() {
        let c = chars[i];
        if c == '`' {
            if let Some(end) = find(&chars, i + 1, &['`']) {
                let inner: String = chars[i + 1..end].iter().collect();
                out.push_str("<span font_family=\"monospace\" background=\"#8080801f\">");
                out.push_str(&escape(&inner));
                out.push_str("</span>");
                i = end + 1;
                continue;
            }
        }
        if c == '*' && chars.get(i + 1) == Some(&'*') {
            if let Some(end) = find(&chars, i + 2, &['*', '*']) {
                if end > i + 2 {
                    let inner: String = chars[i + 2..end].iter().collect();
                    out.push_str(&format!("<b>{}</b>", inline(&inner)));
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
                    out.push_str(&format!("<i>{}</i>", inline(&inner)));
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
                    out.push_str(&format!("<a href=\"{}\">{}</a>", escape(&url), escape(&label)));
                    i = end + 1;
                    continue;
                }
            }
        }
        out.push_str(&escape(&c.to_string()));
        i += 1;
    }
    out
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
