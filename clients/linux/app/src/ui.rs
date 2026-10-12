//! The design's building blocks as GTK widgets: type, turns, bubbles, cards,
//! event lines and markdown. Classes are defined in style.css.

use adw::prelude::*;
use gtk::{glib, pango};

use kit::markdown::{self, Block};
use kit::WorkState;

/// Space tokens, in pixels.
pub const SPACE_1: i32 = 4;
pub const SPACE_2: i32 = 8;
pub const SPACE_3: i32 = 12;
pub const SPACE_4: i32 = 16;
pub const SPACE_5: i32 = 24;
pub const SPACE_6: i32 = 32;
pub const SPACE_7: i32 = 48;

pub fn vbox(spacing: i32) -> gtk::Box {
    gtk::Box::new(gtk::Orientation::Vertical, spacing)
}

pub fn hbox(spacing: i32) -> gtk::Box {
    gtk::Box::new(gtk::Orientation::Horizontal, spacing)
}

/// Plain text, start-aligned and wrapping, in the given classes.
pub fn text(text: &str, classes: &[&str]) -> gtk::Label {
    let label = gtk::Label::builder().label(text).xalign(0.0).wrap(true).wrap_mode(pango::WrapMode::WordChar).build();
    for class in classes {
        label.add_css_class(class);
    }
    label
}

/// One line of text that ends in an ellipsis rather than wrapping.
pub fn line(text: &str, classes: &[&str]) -> gtk::Label {
    let label = gtk::Label::builder().label(text).xalign(0.0).ellipsize(pango::EllipsizeMode::End).build();
    for class in classes {
        label.add_css_class(class);
    }
    label
}

/// A small capitalised label naming a section; never cut short.
pub fn micro(text: &str, classes: &[&str]) -> gtk::Label {
    let label = gtk::Label::builder().label(text.to_uppercase()).xalign(0.0).build();
    label.add_css_class("micro");
    for class in classes {
        label.add_css_class(class);
    }
    label
}

/// Text a reader may want to copy: wrapping and selectable.
pub fn said(text: &str, classes: &[&str]) -> gtk::Label {
    let label = self::text(text, classes);
    label.set_selectable(true);
    label
}

pub fn icon(name: &str, classes: &[&str]) -> gtk::Image {
    let image = gtk::Image::from_icon_name(name);
    for class in classes {
        image.add_css_class(class);
    }
    image
}

/// A state's own icon in its own colour, named for a screen reader.
pub fn state_icon(state: WorkState) -> gtk::Image {
    let look = state.look();
    let image = icon(look.icon, &[look.tint.css_class()]);
    image.set_tooltip_text(Some(look.label));
    image.update_property(&[gtk::accessible::Property::Label(look.label)]);
    image
}

/// A state as a pill: icon and label.
pub fn state_pill(state: WorkState) -> gtk::Box {
    let look = state.look();
    let pill = hbox(SPACE_1);
    pill.add_css_class("pill");
    pill.add_css_class(look.tint.css_class());
    pill.append(&icon(look.icon, &[]));
    pill.append(&gtk::Label::new(Some(look.label)));
    pill
}

/// A small rounded tag.
pub fn tag(text: &str, classes: &[&str]) -> gtk::Label {
    let label = gtk::Label::builder().label(text).valign(gtk::Align::Center).build();
    label.add_css_class("tag");
    for class in classes {
        label.add_css_class(class);
    }
    label
}

/// A button that reads as a link.
pub fn link(text: &str) -> gtk::Button {
    let button = gtk::Button::with_label(text);
    button.add_css_class("flat");
    button.add_css_class("link-button");
    button.set_halign(gtk::Align::Start);
    button
}

/// A turn: the speaker's avatar, the meta line, and the body under it. The
/// body never sits under the avatar. A turn that continues the one before it
/// keeps the avatar column empty and drops the meta line.
pub fn turn(avatar: Option<&gtk::Widget>, meta: Option<&gtk::Widget>, body: &impl IsA<gtk::Widget>) -> gtk::Box {
    let row = hbox(SPACE_3);
    let column = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    column.set_size_request(24, -1);
    column.set_valign(gtk::Align::Start);
    if let Some(avatar) = avatar {
        column.append(avatar);
    }
    row.append(&column);
    let content = vbox(SPACE_2);
    content.set_hexpand(true);
    if let Some(meta) = meta {
        content.append(meta);
    }
    content.append(body);
    row.append(&content);
    row
}

/// A speaker's mark: an icon in a 24 px column.
pub fn avatar(icon_name: &str, classes: &[&str]) -> gtk::Widget {
    let image = icon(icon_name, classes);
    image.set_pixel_size(16);
    image.set_size_request(24, 24);
    image.add_css_class("avatar");
    image.upcast()
}

/// The meta line: speaker · time · detail, the speaker clickable when it leads somewhere.
pub fn meta(speaker: &gtk::Widget, at: Option<&str>, detail: Option<&str>) -> gtk::Widget {
    let row = hbox(SPACE_1);
    row.add_css_class("meta");
    row.append(speaker);
    for part in [at.map(kit::time::when), detail.map(str::to_string)].into_iter().flatten() {
        if part.is_empty() {
            continue;
        }
        row.append(&line(&format!("· {part}"), &["text3"]));
    }
    row.upcast()
}

pub fn speaker(name: &str) -> gtk::Widget {
    line(name, &["speaker"]).upcast()
}

/// Your words: a right-aligned bubble in the accent colour, its meta line above
/// it; long text folds after 8 lines.
pub fn you_bubble(body: &str, label: &str, at: Option<&str>, status: Option<&gtk::Widget>) -> gtk::Box {
    let column = vbox(SPACE_2);
    column.set_halign(gtk::Align::End);
    column.set_margin_start(SPACE_7 * 2);
    let meta = hbox(SPACE_1);
    meta.set_halign(gtk::Align::End);
    meta.add_css_class("meta");
    meta.append(&line(label, &["speaker"]));
    if let Some(at) = at {
        meta.append(&line(&format!("· {}", kit::time::when(at)), &["text3"]));
    }
    if let Some(status) = status {
        meta.append(status);
    }
    column.append(&meta);
    let bubble = vbox(SPACE_2);
    bubble.add_css_class("bubble-you");
    bubble.append(&folded(body, 8, &["body"]));
    column.append(&bubble);
    column
}

/// Text that folds after `lines` lines, with Show all to read the rest.
pub fn folded(body: &str, lines: i32, classes: &[&str]) -> gtk::Box {
    let column = vbox(SPACE_1);
    let label = said(body, classes);
    column.append(&label);
    let long = body.lines().count() > lines as usize || body.chars().count() > (lines as usize) * 110;
    if long {
        label.set_lines(lines);
        label.set_ellipsize(pango::EllipsizeMode::End);
        let more = link("Show all");
        let l = label.clone();
        more.connect_clicked(move |b| {
            let open = l.lines() < 0;
            l.set_lines(if open { lines } else { -1 });
            l.set_ellipsize(if open { pango::EllipsizeMode::End } else { pango::EllipsizeMode::None });
            b.set_label(if open { "Show all" } else { "Show less" });
        });
        column.append(&more);
    }
    column
}

/// A box with a meaning: raised card, attention, settled.
pub fn card(class: &str) -> gtk::Box {
    let card = vbox(SPACE_3);
    card.add_css_class(class);
    card
}

/// A look, a state change or a stop: centred meta text between two hairlines.
pub fn event_line(text: &str, classes: &[&str]) -> gtk::Box {
    let row = hbox(SPACE_3);
    row.add_css_class("event-line");
    let left = gtk::Separator::new(gtk::Orientation::Horizontal);
    left.set_hexpand(true);
    left.set_valign(gtk::Align::Center);
    let right = gtk::Separator::new(gtk::Orientation::Horizontal);
    right.set_hexpand(true);
    right.set_valign(gtk::Align::Center);
    let label = line(text, &["meta", "text3"]);
    label.set_xalign(0.5);
    label.set_max_width_chars(80);
    for class in classes {
        label.add_css_class(class);
    }
    row.append(&left);
    row.append(&label);
    row.append(&right);
    row
}

/// The line that starts each day of a conversation.
pub fn day_line(day: &str) -> gtk::Box {
    event_line(day, &["strong"])
}

/// Monospace text in a sunken block that scrolls sideways rather than wrapping.
pub fn code_block(code: &str) -> gtk::Widget {
    let label = gtk::Label::builder().label(code).xalign(0.0).selectable(true).build();
    label.add_css_class("code");
    let scroller = gtk::ScrolledWindow::builder()
        .hscrollbar_policy(gtk::PolicyType::Automatic)
        .vscrollbar_policy(gtk::PolicyType::Never)
        .child(&label)
        .build();
    scroller.add_css_class("sunken");
    scroller.upcast()
}

/// Markdown as blocks of labels: paragraphs, headings, bullets and code.
pub fn markdown(source: &str) -> gtk::Box {
    let column = vbox(SPACE_3);
    for block in markdown::parse(source) {
        let widget: gtk::Widget = match block {
            Block::Heading(_, title) => {
                let label = marked(&markdown::inline(&title), &["title"]);
                label.set_margin_top(SPACE_2);
                label.upcast()
            }
            Block::Bullet(item, number) => {
                let row = hbox(SPACE_2);
                let marker = gtk::Label::new(Some(number.as_deref().unwrap_or("•")));
                marker.add_css_class("text2");
                marker.set_valign(gtk::Align::Start);
                marker.set_xalign(1.0);
                marker.set_size_request(12, -1);
                let body = marked(&markdown::inline(&item), &["body"]);
                body.set_hexpand(true);
                row.append(&marker);
                row.append(&body);
                row.upcast()
            }
            Block::Code(code) => code_block(&code),
            Block::Paragraph(paragraph) => marked(&markdown::inline(&paragraph), &["body"]).upcast(),
        };
        column.append(&widget);
    }
    column
}

fn marked(markup: &str, classes: &[&str]) -> gtk::Label {
    let label = said("", classes);
    label.set_markup(markup);
    label
}

/// Text for Pango markup.
pub fn escaped(text: &str) -> String {
    glib::markup_escape_text(text).to_string()
}

/// A centred empty or error state: icon, one title line, one line of detail.
pub fn empty_state(icon_name: &str, title: &str, detail: &str) -> adw::StatusPage {
    adw::StatusPage::builder().icon_name(icon_name).title(title).description(escaped(detail)).build()
}

/// A spinner with what it is waiting on.
pub fn waiting(what: &str) -> gtk::Box {
    let row = hbox(SPACE_2);
    let spinner = gtk::Spinner::new();
    spinner.start();
    row.append(&spinner);
    row.append(&line(what, &["callout", "text2"]));
    row
}

/// A failure, in red, selectable so it can be copied.
pub fn failure(why: &str) -> gtk::Box {
    let row = hbox(SPACE_2);
    row.append(&icon("dialog-warning-symbolic", &["failure"]));
    let label = said(why, &["callout", "failure"]);
    label.set_hexpand(true);
    row.append(&label);
    row
}

/// Removes every child of a box.
pub fn clear(container: &gtk::Box) {
    while let Some(child) = container.first_child() {
        container.remove(&child);
    }
}
