//! Which wd the app runs. A wd found on PATH could be any version, and the app
//! reads exactly the wire of the wd it was packaged with.

use std::ffi::OsStr;
use std::fmt;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LocatorError {
    /// Where the packaged wd was looked for.
    pub beside: PathBuf,
}

impl fmt::Display for LocatorError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "no wd to run: none at {} beside the app, and WD_BIN is not set; reinstall the app or build it with scripts/build-linux.sh",
            self.beside.display()
        )
    }
}

impl std::error::Error for LocatorError {}

/// The wd installed beside the app's executable, else `$WD_BIN`.
pub fn locate_wd(exe: &Path, wd_bin: Option<&OsStr>) -> Result<PathBuf, LocatorError> {
    let beside = exe.parent().unwrap_or(Path::new("/")).join("wd");
    let runnable = std::fs::metadata(&beside).is_ok_and(|m| m.is_file() && m.permissions().mode() & 0o111 != 0);
    if runnable {
        return Ok(beside);
    }
    match wd_bin {
        Some(path) if !path.is_empty() => Ok(PathBuf::from(path)),
        _ => Err(LocatorError { beside }),
    }
}
