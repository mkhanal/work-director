//! A panel beside the page: the page narrows to make room for it, its edge
//! drags to resize it, and it hides and shows. Only while the goals sidebar is
//! folded away, the window too narrow for both, does it open over the page.

use std::cell::Cell;
use std::rc::Rc;

use adw::prelude::*;
use gtk::subclass::prelude::*;
use gtk::{gdk, glib};

use kit::PanelWidth;

/// How far one press of an arrow key on the edge moves it.
const STEP: f64 = 16.0;

pub struct SidePanel {
    pub widget: gtk::Overlay,
    row: gtk::Box,
    /// The edge and the panel, together: beside the page, or over it.
    side: gtk::Box,
    holder: gtk::Box,
    layout: PanelRow,
    width: Cell<PanelWidth>,
    over: Cell<bool>,
}

/// The widths of the page and of the side, out of `total`: the side as wide as
/// chosen while the page keeps its minimum, and never narrower than its own.
fn split(total: i32, page_min: i32, side_min: i32, chosen: i32) -> (i32, i32) {
    let side = chosen.min(total - page_min).max(side_min);
    ((total - side).max(0), side)
}

/// Lays the page and the side out in a row, the side at the width chosen.
mod imp {
    use std::cell::Cell;

    use gtk::glib;
    use gtk::prelude::*;
    use gtk::subclass::prelude::*;

    use super::split;

    #[derive(Default)]
    pub struct PanelRow {
        pub chosen: Cell<i32>,
    }

    #[glib::object_subclass]
    impl ObjectSubclass for PanelRow {
        const NAME: &'static str = "WdPanelRow";
        type Type = super::PanelRow;
        type ParentType = gtk::LayoutManager;
    }

    impl ObjectImpl for PanelRow {}

    impl PanelRow {
        /// The page, and the side while it shows.
        fn children(row: &gtk::Widget) -> Option<(gtk::Widget, Option<gtk::Widget>)> {
            let page = row.first_child()?;
            let side = page.next_sibling().filter(|s| s.is_visible());
            Some((page, side))
        }

        /// The page's and the side's widths out of `total`.
        fn widths(&self, page: &gtk::Widget, side: &gtk::Widget, total: i32) -> (i32, i32) {
            let page_min = page.measure(gtk::Orientation::Horizontal, -1).0;
            let side_min = side.measure(gtk::Orientation::Horizontal, -1).0;
            split(total, page_min, side_min, self.chosen.get().max(side_min))
        }
    }

    impl LayoutManagerImpl for PanelRow {
        fn request_mode(&self, _row: &gtk::Widget) -> gtk::SizeRequestMode {
            gtk::SizeRequestMode::HeightForWidth
        }

        fn measure(&self, row: &gtk::Widget, orientation: gtk::Orientation, for_size: i32) -> (i32, i32, i32, i32) {
            let Some((page, side)) = Self::children(row) else { return (0, 0, -1, -1) };
            let Some(side) = side else {
                let (min, nat, _, _) = page.measure(orientation, for_size);
                return (min, nat, -1, -1);
            };
            if orientation == gtk::Orientation::Horizontal {
                let (page_min, page_nat, _, _) = page.measure(orientation, -1);
                let side_min = side.measure(orientation, -1).0;
                return (page_min + side_min, page_nat + self.chosen.get().max(side_min), -1, -1);
            }
            let (page_w, side_w) = if for_size < 0 { (-1, -1) } else { self.widths(&page, &side, for_size) };
            let (a_min, a_nat, _, _) = page.measure(orientation, page_w);
            let (b_min, b_nat, _, _) = side.measure(orientation, side_w);
            (a_min.max(b_min), a_nat.max(b_nat), -1, -1)
        }

        fn allocate(&self, row: &gtk::Widget, width: i32, height: i32, baseline: i32) {
            let Some((page, side)) = Self::children(row) else { return };
            let Some(side) = side else { return page.allocate(width, height, baseline, None) };
            let (page_w, side_w) = self.widths(&page, &side, width);
            page.allocate(page_w, height, baseline, None);
            let at = gtk::gsk::Transform::new().translate(&gtk::graphene::Point::new(page_w as f32, 0.0));
            side.allocate(side_w, height, baseline, Some(at));
        }
    }
}

glib::wrapper! {
    pub struct PanelRow(ObjectSubclass<imp::PanelRow>) @extends gtk::LayoutManager;
}

impl PanelRow {
    fn new(chosen: PanelWidth) -> PanelRow {
        let row: PanelRow = glib::Object::new();
        row.choose(chosen);
        row
    }

    fn choose(&self, chosen: PanelWidth) {
        self.imp().chosen.set(chosen.0);
        self.layout_changed();
    }
}

impl SidePanel {
    pub fn new(
        content: &impl IsA<gtk::Widget>,
        panel: &impl IsA<gtk::Widget>,
        width: PanelWidth,
        shown: bool,
        resized: impl Fn(PanelWidth) + 'static,
    ) -> Rc<SidePanel> {
        let layout = PanelRow::new(width);
        let row = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        row.set_layout_manager(Some(layout.clone()));
        row.append(content);

        let edge = gtk::Separator::new(gtk::Orientation::Vertical);
        edge.add_css_class("panel-edge");
        edge.set_focusable(true);
        edge.set_cursor_from_name(Some("col-resize"));
        edge.set_tooltip_text(Some("Drag to resize the agents panel"));
        edge.update_property(&[gtk::accessible::Property::Label("Resize the agents panel")]);
        let holder = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        holder.set_hexpand(true);
        holder.set_size_request(PanelWidth::MIN, -1);
        holder.append(panel);
        panel.set_hexpand(true);
        let side = gtk::Box::new(gtk::Orientation::Horizontal, 0);
        side.add_css_class("side-panel");
        side.append(&edge);
        side.append(&holder);
        side.set_visible(shown);
        row.append(&side);

        let widget = gtk::Overlay::new();
        widget.set_child(Some(&row));

        let me =
            Rc::new(SidePanel { widget, row, side, holder, layout, width: Cell::new(width), over: Cell::new(false) });

        let resized: Rc<dyn Fn(PanelWidth)> = Rc::new(resized);
        let drag = gtk::GestureDrag::new();
        let from = Rc::new(Cell::new(me.width.get()));
        let (weak, start) = (Rc::downgrade(&me), Rc::clone(&from));
        drag.connect_drag_begin(move |_, _, _| {
            if let Some(me) = weak.upgrade() {
                start.set(me.width.get());
            }
        });
        let weak = Rc::downgrade(&me);
        drag.connect_drag_update(move |_, dx, _| {
            if let Some(me) = weak.upgrade() {
                me.apply(from.get().dragged(dx));
            }
        });
        let (weak, done) = (Rc::downgrade(&me), Rc::clone(&resized));
        drag.connect_drag_end(move |_, _, _| {
            if let Some(me) = weak.upgrade() {
                done(me.width.get());
            }
        });
        edge.add_controller(drag);

        let keys = gtk::EventControllerKey::new();
        let weak = Rc::downgrade(&me);
        keys.connect_key_pressed(move |_, key, _, _| {
            let dx = match key {
                gdk::Key::Left => -STEP,
                gdk::Key::Right => STEP,
                _ => return glib::Propagation::Proceed,
            };
            if let Some(me) = weak.upgrade() {
                me.apply(me.width.get().dragged(dx));
                resized(me.width.get());
            }
            glib::Propagation::Stop
        });
        edge.add_controller(keys);
        me
    }

    fn apply(&self, width: PanelWidth) {
        self.width.set(width);
        if self.over.get() {
            self.holder.set_size_request(width.0, -1);
        } else {
            self.layout.choose(width);
        }
    }

    pub fn set_shown(&self, shown: bool) {
        self.side.set_visible(shown);
    }

    /// Puts the panel over the page, or back beside it.
    pub fn set_over(&self, over: bool) {
        if self.over.replace(over) == over {
            return;
        }
        if over {
            self.row.remove(&self.side);
            self.holder.set_size_request(self.width.get().0, -1);
            self.side.set_halign(gtk::Align::End);
            self.side.add_css_class("over");
            self.widget.add_overlay(&self.side);
        } else {
            self.widget.remove_overlay(&self.side);
            self.holder.set_size_request(PanelWidth::MIN, -1);
            self.layout.choose(self.width.get());
            self.side.set_halign(gtk::Align::Fill);
            self.side.remove_css_class("over");
            self.row.append(&self.side);
        }
    }
}
