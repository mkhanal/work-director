//! Finding words on a page: where a query occurs in what the page shows,
//! case and accents ignored, and which match is current as the person steps
//! through them and the page refreshes under them.

use unicode_normalization::char::is_combining_mark;
use unicode_normalization::UnicodeNormalization;

/// A character as find compares it: without its accents, in lower case. One
/// character can fold to several, or to none.
pub fn fold(c: char) -> impl Iterator<Item = char> {
    std::iter::once(c).nfd().filter(|d| !is_combining_mark(*d)).flat_map(char::to_lowercase)
}

/// A query as find compares it.
pub fn needle(query: &str) -> Vec<char> {
    query.chars().flat_map(fold).collect()
}

/// What a page shows, folded: each character with the offset it was folded
/// from, and `None` where something no match may cross stands: a paragraph's
/// end, an icon, a widget, words hidden by a closed fold.
#[derive(Debug, Default)]
pub struct Haystack(Vec<Option<(char, usize)>>);

impl Haystack {
    /// Adds the words starting at offset `at`.
    pub fn words(&mut self, text: &str, at: usize) {
        for (i, c) in text.chars().enumerate() {
            self.0.extend(fold(c).map(|f| Some((f, at + i))));
        }
    }

    pub fn wall(&mut self) {
        if self.0.last().is_some_and(Option::is_some) {
            self.0.push(None);
        }
    }

    /// Each place `needle` occurs, as the offsets it covers, start to end;
    /// matches never overlap and start and end on whole characters.
    pub fn find(&self, needle: &[char]) -> Vec<(usize, usize)> {
        let (hay, n) = (&self.0, needle.len());
        let mut found = Vec::new();
        if n == 0 {
            return found;
        }
        let origin = |i: usize| hay.get(i).copied().flatten().map(|(_, at)| at);
        let mut i = 0;
        while i + n <= hay.len() {
            let fits = hay[i..i + n].iter().zip(needle).all(|(h, c)| h.is_some_and(|(f, _)| f == *c));
            let (start, end) = (origin(i), origin(i + n - 1));
            let whole = i == 0 || origin(i - 1) != start;
            let whole_end = origin(i + n) != end;
            match (fits && whole && whole_end, start, end) {
                (true, Some(a), Some(b)) => {
                    found.push((a, b + 1));
                    i += n;
                }
                _ => i += 1,
            }
        }
        found
    }
}

/// The match after or before `current` among `count`, wrapping round; the
/// first going forward from none, the last going back.
pub fn step(current: Option<usize>, count: usize, forward: bool) -> Option<usize> {
    if count == 0 {
        return None;
    }
    Some(match (current, forward) {
        (None, true) => 0,
        (None, false) => count - 1,
        (Some(i), true) => (i + 1) % count,
        (Some(i), false) => (i + count - 1) % count,
    })
}

/// The match that stays current when the matches change, as the person types
/// or the page refreshes: the first at or after where the current one was,
/// else the last; the first when none was.
pub fn keep<T: Ord>(was: Option<&T>, at: &[T]) -> Option<usize> {
    if at.is_empty() {
        return None;
    }
    let Some(was) = was else { return Some(0) };
    Some(at.iter().position(|a| a >= was).unwrap_or(at.len() - 1))
}

/// What the find bar says of the matches.
pub fn label(current: Option<usize>, count: usize) -> String {
    match (current, count) {
        (_, 0) => "No results".into(),
        (Some(i), n) => format!("{} of {n}", i + 1),
        (None, n) => format!("{n} found"),
    }
}
