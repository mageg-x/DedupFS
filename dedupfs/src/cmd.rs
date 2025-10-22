use crate::daemon::{Command, Response};
use std::path::{ PathBuf, Path };
use anyhow::Result;
use tracing;

pub fn handle_mount(cmd: Command) -> Result<Response> {
    if cmd.name == "mount" && cmd.args.len() >= 2 {
        let mount_point = &cmd.args[0];
        let data_dir = &cmd.args[1];
        
        tracing::info!("mounting filesystem at: {}, data stored at: {}", mount_point, data_dir);
        
        match crate::mount::mount_filesystem(Path::new(mount_point), Path::new(data_dir)) {
            Ok(msg) => {
                tracing::info!("{}", msg);
                Ok(Response::Data(msg))
            },
            Err(e) => {
                tracing::error!("failed to mount filesystem: {}", e);
                Ok(Response::Error(format!("failed to mount filesystem: {}", e)))
            }
        }
    } else {
        Err(crate::errors::new(crate::errors::InvalidArguments { command: "mount command".to_string() }))
    }
}

pub fn handle_unmount(cmd: Command) -> Result<Response> {
    if cmd.name == "unmount" && cmd.args.len() >= 1 {
        let mount_point = &cmd.args[0];
        let path = Path::new(mount_point);
        
        tracing::info!("unmounting filesystem at: {}", mount_point);
        
        match crate::mount::unmount_filesystem(path) {
            Ok(msg) => {
                tracing::info!("{}", msg);
                Ok(Response::Data(msg))
            },
            Err(e) => {
                tracing::error!("failed to unmount filesystem: {}", e);
                Ok(Response::Error(format!("failed to unmount filesystem: {}", e)))
            }
        }
    } else {
        Err(crate::errors::new(crate::errors::InvalidArguments { command: "unmount command".to_string() }))
    }
}

pub fn handle_stats(cmd: Command) -> Result<Response> {
    if cmd.name == "stats" && cmd.args.len() >= 1 {
        let mount_point = &cmd.args[0];
        tracing::info!("getting stats for: {}", mount_point);
        // Simulate stats operation
        std::thread::sleep(std::time::Duration::from_millis(100));
        Ok(Response::Data(format!("total blocks: 12345, space saved: 102.5 MB")))
    } else {
        Err(crate::errors::new(crate::errors::InvalidArguments { command: "stats command".to_string() }))
    }
}