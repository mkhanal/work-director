//! The design's building blocks as GTK widgets: type, turns, bubbles, cards,
//! event lines and markdown. Classes are defined in style.css.

use adw::prelude::*;
use gtk::{gdk, gio, glib, pango};

use kit::markdown::{self, Block};
use kit::{Mark, Tone, WorkState};

/// Space tokens, in pixels.
pub const SPACE_1: i32 = 4;
pub const SPACE_2: i32 = 8;
pub const SPACE_3: i32 = 12;
pub const SPACE_4: i32 = 16;
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

/// A file's name on one line: as wide as it is, up to 28 characters, then cut in the middle.
pub fn name_line(name: &str, classes: &[&str]) -> gtk::Label {
    let label = line(name, classes);
    label.set_ellipsize(pango::EllipsizeMode::Middle);
    label.set_width_chars(name.chars().count().min(28) as i32);
    label.set_max_width_chars(28);
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

/// Markdown as blocks of labels: paragraphs, headings, bullets, code,
/// tables, quotes and rules.
pub fn markdown(source: &str) -> gtk::Box {
    blocks(markdown::parse(source))
}

fn blocks(parsed: Vec<Block>) -> gtk::Box {
    let column = vbox(SPACE_3);
    for block in parsed {
        let widget: gtk::Widget = match block {
            Block::Heading(_, title) => {
                let label = marked(&markdown::inline(&title), &["title"]);
                label.set_margin_top(SPACE_2);
                label.upcast()
            }
            Block::Bullet { text, number, depth } => {
                let row = hbox(SPACE_2);
                row.set_margin_start(depth as i32 * SPACE_4);
                let marker = gtk::Label::new(Some(number.as_deref().unwrap_or(if depth == 0 { "•" } else { "◦" })));
                marker.add_css_class("text2");
                marker.set_valign(gtk::Align::Start);
                marker.set_xalign(1.0);
                marker.set_size_request(12, -1);
                let body = marked(&markdown::inline(&text), &["body"]);
                body.set_hexpand(true);
                row.append(&marker);
                row.append(&body);
                row.upcast()
            }
            Block::Code(code) => code_block(&code),
            Block::Paragraph(paragraph) => marked(&markdown::inline(&paragraph), &["body"]).upcast(),
            Block::Table(table) => self::table(&table).upcast(),
            Block::Quote(inner) => {
                let quote = blocks(inner);
                quote.add_css_class("md-quote");
                quote.upcast()
            }
            Block::Rule => hairline().upcast(),
        };
        column.append(&widget);
    }
    column
}

/// A markdown table as a grid: the header semibold, a hairline under each
/// row, each cell aligned as its column and wrapping within it.
pub fn table(table: &markdown::Table) -> gtk::Widget {
    let grid = gtk::Grid::new();
    grid.add_css_class("md-table");
    grid.set_halign(gtk::Align::Start);
    let last = table.rows.len();
    for (r, row) in std::iter::once(&table.head).chain(&table.rows).enumerate() {
        for (c, cell) in row.iter().enumerate() {
            let align = table.align.get(c).copied().unwrap_or(markdown::Align::Start);
            let (x, justify) = match align {
                markdown::Align::Start => (0.0, gtk::Justification::Left),
                markdown::Align::Center => (0.5, gtk::Justification::Center),
                markdown::Align::End => (1.0, gtk::Justification::Right),
            };
            let label = gtk::Label::builder()
                .use_markup(true)
                .label(markdown::inline(cell))
                .xalign(x)
                .yalign(0.0)
                .justify(justify)
                .wrap(true)
                .wrap_mode(pango::WrapMode::WordChar)
                .natural_wrap_mode(gtk::NaturalWrapMode::Word)
                .max_width_chars(48)
                .hexpand(true)
                .build();
            label.add_css_class("md-cell");
            if r == 0 {
                label.add_css_class("md-head");
            } else if r == last {
                label.add_css_class("last");
            }
            grid.attach(&label, c as i32, r as i32, 1, 1);
        }
    }
    grid.upcast()
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

/// A badge: a short word in its tone's colours.
pub fn badge(text: &str, tone: Tone) -> gtk::Label {
    let label = gtk::Label::builder().label(text).valign(gtk::Align::Center).halign(gtk::Align::Start).build();
    label.add_css_class("badge");
    label.add_css_class(tone.css_class());
    label
}

/// A state as a badge.
pub fn state_badge(state: WorkState) -> gtk::Label {
    let (text, tone) = state.badge();
    badge(text, tone)
}

/// The drawn state icon, `size` pixels square, coloured by its class and
/// named for a screen reader.
pub fn mark(mark: Mark, size: i32, name: &str) -> gtk::DrawingArea {
    let area = gtk::DrawingArea::builder().content_width(size).content_height(size).valign(gtk::Align::Start).build();
    area.add_css_class(mark.css_class());
    area.update_property(&[gtk::accessible::Property::Label(name)]);
    area.set_tooltip_text(Some(name));
    area.set_draw_func(move |area, cr, w, h| {
        let c = area.color();
        let (r, g, b) = (f64::from(c.red()), f64::from(c.green()), f64::from(c.blue()));
        let unit = f64::from(w.min(h)) / 20.0;
        let (cx, cy) = (f64::from(w) / 2.0, f64::from(h) / 2.0);
        cr.set_source_rgb(r, g, b);
        cr.set_line_width(2.0 * unit);
        let ring = |cr: &gtk::cairo::Context| {
            cr.new_path();
            cr.arc(cx, cy, 8.0 * unit, 0.0, std::f64::consts::TAU);
            let _ = cr.stroke();
        };
        // A failed stroke or fill leaves the icon blank; there is nothing else to draw instead.
        match mark {
            Mark::Done => {
                cr.arc(cx, cy, 9.0 * unit, 0.0, std::f64::consts::TAU);
                let _ = cr.fill();
                cr.set_source_rgb(1.0, 1.0, 1.0);
                cr.move_to(cx - 4.0 * unit, cy + 0.5 * unit);
                cr.line_to(cx - 1.4 * unit, cy + 3.0 * unit);
                cr.line_to(cx + 4.0 * unit, cy - 2.5 * unit);
                let _ = cr.stroke();
            }
            Mark::NeedsYou => {
                ring(cr);
                cr.arc(cx, cy, 3.0 * unit, 0.0, std::f64::consts::TAU);
                let _ = cr.fill();
            }
            Mark::Working => {
                ring(cr);
                cr.move_to(cx, cy);
                cr.line_to(cx, cy - 8.0 * unit);
                cr.arc(cx, cy, 8.0 * unit, -std::f64::consts::FRAC_PI_2, 0.0);
                cr.close_path();
                let _ = cr.fill();
            }
            Mark::Waiting => {
                cr.set_dash(&[3.0 * unit, 3.0 * unit], 0.0);
                ring(cr);
            }
            Mark::Superseded | Mark::Blocked => {
                ring(cr);
                cr.move_to(cx - 5.0 * unit, cy);
                cr.line_to(cx + 5.0 * unit, cy);
                let _ = cr.stroke();
            }
        }
    });
    area
}

/// A state's drawn icon, named by its label.
pub fn state_mark(state: WorkState, size: i32) -> gtk::DrawingArea {
    mark(state.mark(), size, state.mark().label())
}

/// A bordered card in the canvas's shape.
pub fn wd_card(spacing: i32) -> gtk::Box {
    let card = vbox(spacing);
    card.add_css_class("wd-card");
    card
}

/// A card's label: 12 px, medium, third text colour.
pub fn label(text: &str) -> gtk::Label {
    let label = gtk::Label::builder().label(text).xalign(0.0).wrap(true).build();
    label.add_css_class("label");
    label
}

/// A sub-goal's path as a small tag.
pub fn sgtag(text: &str) -> gtk::Label {
    let label = gtk::Label::builder().label(text).valign(gtk::Align::Center).build();
    label.add_css_class("sgtag");
    label
}

/// A button in the canvas's shape; `primary` fills it with the accent.
pub fn button(text: &str, primary: bool) -> gtk::Button {
    let button = gtk::Button::with_label(text);
    button.add_css_class("wd-button");
    if primary {
        button.add_css_class("primary");
    }
    button.set_valign(gtk::Align::Center);
    button
}

/// A choice pressed as one button: its radio or check mark and its words
/// together, announced by `name` with what it is, so assistive technology can
/// tell choices apart and press one. A radio choice joins `group`, the first
/// radio of its question, when one is given.
pub fn choice(
    words: &impl IsA<gtk::Widget>,
    name: &str,
    radio: bool,
    group: Option<&gtk::ToggleButton>,
) -> gtk::ToggleButton {
    let role = if radio { gtk::AccessibleRole::Radio } else { gtk::AccessibleRole::Checkbox };
    let button: gtk::ToggleButton = glib::Object::builder().property("accessible-role", role).build();
    button.add_css_class("choice");
    // The mark only shows the button's state; the button is what is pressed and announced.
    let mark: gtk::CheckButton =
        glib::Object::builder().property("accessible-role", gtk::AccessibleRole::Presentation).build();
    mark.set_can_target(false);
    mark.set_focusable(false);
    mark.set_valign(gtk::Align::Start);
    if radio {
        // A lone check button draws as a box; one in a group draws as a radio.
        mark.set_group(Some(&gtk::CheckButton::new()));
    }
    if let Some(first) = group {
        button.set_group(Some(first));
    }
    let row = hbox(SPACE_2);
    row.append(&mark);
    row.append(words);
    button.set_child(Some(&row));
    button.update_property(&[gtk::accessible::Property::Label(name)]);
    button.connect_toggled(move |b| mark.set_active(b.is_active()));
    button
}

/// Names a control for assistive technology by `said` alone: not by the label
/// it holds, nor, for a menu button, by the text of its popover.
pub fn announce(control: &impl IsA<gtk::Widget>, said: &str) {
    let control = control.upcast_ref::<gtk::Widget>();
    let mut named = vec![control.clone()];
    if control.is::<gtk::MenuButton>() {
        // A menu button is pressed through the toggle it holds; that is what is announced.
        named.extend(control.first_child().filter(|c| c.is::<gtk::ToggleButton>()));
    }
    for widget in named {
        widget.reset_relation(gtk::AccessibleRelation::LabelledBy);
        widget.update_property(&[gtk::accessible::Property::Label(said)]);
    }
}

/// A small link inside a line of text.
pub fn inline_link(text: &str) -> gtk::Button {
    let button = gtk::Button::with_label(text);
    button.add_css_class("inline-link");
    button.set_valign(gtk::Align::Center);
    button
}

/// A filter chip; chips in one row share a group so one is chosen.
pub fn fchip(text: &str, chosen: bool, group: Option<&gtk::ToggleButton>) -> gtk::ToggleButton {
    // A chip is never narrower than its words, up to 28 characters.
    let chars = text.chars().count().min(28) as i32;
    let label = gtk::Label::builder()
        .label(text)
        .width_chars(chars)
        .max_width_chars(28)
        .ellipsize(pango::EllipsizeMode::End)
        .build();
    let chip = gtk::ToggleButton::builder().child(&label).active(chosen).tooltip_text(text).build();
    chip.add_css_class("fchip");
    if let Some(group) = group {
        chip.set_group(Some(group));
    }
    chip
}

/// A row of chips that scrolls sideways when the page is narrow; `row` is
/// what the chips go in.
pub fn chip_row() -> (gtk::ScrolledWindow, gtk::Box) {
    let row = hbox(6);
    let scroller = gtk::ScrolledWindow::builder()
        .hscrollbar_policy(gtk::PolicyType::Automatic)
        .vscrollbar_policy(gtk::PolicyType::Never)
        .propagate_natural_width(true)
        .propagate_natural_height(true)
        .hexpand(true)
        .child(&row)
        .build();
    (scroller, row)
}

/// Done of total as a 120 by 4 bar.
pub fn progress_bar(done: i64, total: i64) -> gtk::ProgressBar {
    let bar = gtk::ProgressBar::new();
    bar.add_css_class("wd-progress");
    bar.set_valign(gtk::Align::Center);
    bar.set_size_request(120, 4);
    bar.set_fraction(if total > 0 { done as f64 / total as f64 } else { 0.0 });
    bar.update_property(&[gtk::accessible::Property::Label(&format!("{done} of {total} done"))]);
    bar
}

/// A horizontal hairline.
pub fn hairline() -> gtk::Box {
    let line = gtk::Box::new(gtk::Orientation::Horizontal, 0);
    line.add_css_class("hairline");
    line
}

/// Removes every child of a box.
pub fn clear(container: &gtk::Box) {
    while let Some(child) = container.first_child() {
        container.remove(&child);
    }
}

/// Makes every label under `root` selectable, so its text can be copied,
/// except where a click on it does something else: inside a button, an
/// activatable row, an expander's heading or a card that drags. A selectable
/// label is left out of keyboard focus, so Tab still moves between controls;
/// `extra` joins each label's own menu.
pub fn copyable(root: &impl IsA<gtk::Widget>, extra: Option<&gio::MenuModel>) {
    walk(root.upcast_ref(), false, extra);
}

/// Whether a click on `widget` does something of its own.
fn pressable(widget: &gtk::Widget) -> bool {
    if let Some(row) = widget.downcast_ref::<gtk::ListBoxRow>() {
        return row.is_activatable();
    }
    widget.is::<gtk::Button>()
        || widget.is::<gtk::MenuButton>()
        || widget.is::<gtk::CheckButton>()
        || widget.is::<gtk::DropDown>()
        || widget.has_css_class("draggable")
}

fn walk(widget: &gtk::Widget, pressed: bool, extra: Option<&gio::MenuModel>) {
    if let Some(label) = widget.downcast_ref::<gtk::Label>() {
        if !pressed && label.can_target() && !label.has_css_class("copyable") {
            selectable(label);
            if let Some(extra) = extra {
                label.set_extra_menu(Some(extra));
            }
        }
        return;
    }
    let pressed = pressed || pressable(widget);
    if let Some(expander) = widget.downcast_ref::<gtk::Expander>() {
        // Its heading opens and closes it; what it holds is read.
        if let Some(child) = expander.child() {
            walk(&child, pressed, extra);
        }
        return;
    }
    let mut child = widget.first_child();
    while let Some(c) = child {
        walk(&c, pressed, extra);
        child = c.next_sibling();
    }
}

/// Makes a label's text selectable with the pointer and copied with Ctrl+C,
/// without making it a stop for Tab: it takes focus only from a click, and
/// gives it up when focus moves on.
fn selectable(label: &gtk::Label) {
    label.add_css_class("copyable");
    label.set_selectable(true);
    label.set_focusable(false);
    let press = gtk::GestureClick::new();
    press.set_button(0);
    press.set_propagation_phase(gtk::PropagationPhase::Capture);
    press.connect_pressed(|g, _, _, _| {
        if let Some(label) = g.widget() {
            label.set_focusable(true);
        }
    });
    label.add_controller(press);
    let focus = gtk::EventControllerFocus::new();
    focus.connect_leave(|f| {
        if let Some(label) = f.widget().and_downcast::<gtk::Label>() {
            label.select_region(0, 0);
            label.set_focusable(false);
        }
    });
    label.add_controller(focus);
}

/// Puts text on the clipboard.
pub fn copy_text(widget: &impl IsA<gtk::Widget>, text: &str) {
    widget.clipboard().set_text(text);
}

/// A right-click or long-press menu on `widget` with the items `menu` holds.
pub fn context_menu(widget: &impl IsA<gtk::Widget>, menu: &gio::MenuModel) {
    let widget = widget.upcast_ref::<gtk::Widget>();
    let show = {
        let (weak, menu) = (widget.downgrade(), menu.clone());
        move |x: f64, y: f64| {
            let Some(widget) = weak.upgrade() else { return };
            let popover = gtk::PopoverMenu::from_model(Some(&menu));
            popover.set_parent(&widget);
            popover.set_has_arrow(false);
            popover.set_pointing_to(Some(&gdk::Rectangle::new(x as i32, y as i32, 1, 1)));
            popover.connect_closed(|p| {
                let p = p.clone();
                glib::idle_add_local_once(move || p.unparent());
            });
            popover.popup();
        }
    };
    let click = gtk::GestureClick::new();
    click.set_button(gdk::BUTTON_SECONDARY);
    let shown = show.clone();
    click.connect_pressed(move |g, _, x, y| {
        g.set_state(gtk::EventSequenceState::Claimed);
        shown(x, y);
    });
    widget.add_controller(click);
    let press = gtk::GestureLongPress::new();
    press.set_touch_only(true);
    press.connect_pressed(move |_, x, y| show(x, y));
    widget.add_controller(press);
}

/// A menu with one Copy that puts `text` on the clipboard.
pub fn copy_menu(text: &str) -> gio::MenuModel {
    let menu = gio::Menu::new();
    let item = gio::MenuItem::new(Some("Copy"), None);
    item.set_action_and_target_value(Some("win.copy-text"), Some(&text.to_variant()));
    menu.append_item(&item);
    menu.upcast()
}

/// Files sent with a message: an image as a thumbnail that opens in the
/// person's default app, any other file as a chip that opens its folder.
pub fn attachments(files: &[kit::Attachment]) -> gtk::Box {
    let flow = hbox(SPACE_2);
    flow.set_halign(gtk::Align::End);
    flow.add_css_class("attachments");
    for file in files {
        let path = std::path::Path::new(&file.path);
        let (child, opens, tip): (gtk::Widget, &std::path::Path, String) = if file.is_image() {
            let picture = gtk::Picture::for_filename(path);
            picture.set_content_fit(gtk::ContentFit::Cover);
            picture.set_can_shrink(true);
            picture.set_size_request(120, 90);
            (picture.upcast(), path, format!("Open {}", file.name))
        } else {
            let chip = hbox(SPACE_2);
            chip.append(&icon("text-x-generic-symbolic", &["text2"]));
            chip.append(&name_line(&file.name, &["callout"]));
            chip.append(&gtk::Label::builder().label(file.size_label()).css_classes(["meta", "text3"]).build());
            (chip.upcast(), path.parent().unwrap_or(path), format!("Show {} in its folder", file.name))
        };
        let button = gtk::Button::builder().child(&child).tooltip_text(&tip).build();
        button.add_css_class("attachment");
        button.set_valign(gtk::Align::Start);
        announce(&button, &tip);
        let uri = gio::File::for_path(opens).uri();
        button.connect_clicked(move |b| {
            if let Err(e) = gio::AppInfo::launch_default_for_uri(&uri, gio::AppLaunchContext::NONE) {
                let _ = b.activate_action("win.complain", Some(&format!("cannot open {uri}: {e}").to_variant()));
            }
        });
        flow.append(&button);
    }
    flow
}
