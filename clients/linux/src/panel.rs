//! The agents panel's width: dragged by the person, kept between a floor and
//! a ceiling, remembered between launches.

/// The panel's width in pixels, always within [`PanelWidth::MIN`, `PanelWidth::MAX`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PanelWidth(pub i32);

impl PanelWidth {
    pub const MIN: i32 = 260;
    pub const MAX: i32 = 480;
    pub const DEFAULT: PanelWidth = PanelWidth(320);
    /// The window width, in sp, at and below which the goals sidebar folds
    /// away; only there does the panel open over the page.
    pub const NARROW: f64 = 760.0;

    fn clamped(width: i32) -> PanelWidth {
        PanelWidth(width.clamp(Self::MIN, Self::MAX))
    }

    /// The width as the settings keep it; the default when none is kept or it is not a number.
    pub fn remembered(setting: Option<&str>) -> PanelWidth {
        setting.and_then(|s| s.trim().parse::<i32>().ok()).map_or(Self::DEFAULT, Self::clamped)
    }

    pub fn setting(self) -> String {
        self.0.to_string()
    }

    /// The width after its page-side edge moves `dx` pixels: toward the page widens it.
    pub fn dragged(self, dx: f64) -> PanelWidth {
        Self::clamped(self.0 - dx.round() as i32)
    }

    /// Whether, at this window width in sp, the panel opens over the page.
    pub fn over_page(window_width: f64) -> bool {
        window_width <= Self::NARROW
    }
}
