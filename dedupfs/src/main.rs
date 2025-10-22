// main.rs
#![allow(deprecated)]
#![allow(dead_code)]
#![allow(unused_must_use)]
#![allow(unused_variables)]
#![allow(unused_imports)]
#![allow(unused_assignments)]
mod config;
mod inode;
mod chunk;
mod block;
mod cache;
mod utils;
mod mount;
mod vfile;
mod daemon;
mod cmd;
mod errors;
mod logging;
mod kvstore;

use anyhow::Result;
use clap::{ Parser, Subcommand };
use std::path::{ PathBuf };
use daemon::{ Client, Command as DaemonCommand, Response as DaemonResponse };

// 全局信号处理已移至cmd模块中实现

#[derive(Parser)]
#[command(name = "dedupfs")]
#[command(about = "A high-performance deduplication filesystem", long_about = None)]
struct Cli {
    #[command(subcommand)]
    command: Commands,

    /// 增加详细程度（多次使用可提高详细级别：-v=warning, -vv=info, -vvv=debug, -vvvv=trace）
    #[arg(short, long, action = clap::ArgAction::Count, global = true)]
    verbose: u8,
}

#[derive(Subcommand)]
enum Commands {
    Mount {
        mount_point: PathBuf,
        data_dir: PathBuf,
    },
    Unmount {
        mount_point: PathBuf,
    },
    Stats {
        mount_point: PathBuf,
    },
}

fn main() -> Result<()> {
    // 正常模式，解析CLI参数
    let cli = Cli::parse();
    logging::init(cli.verbose);

    // For Unix systems
    let socket_path = "/tmp/dedupfs.sock";

    // 运行daemon
    if !daemon::is_daemon_running(socket_path) {
        tracing::info!("background service not running, starting as daemon...")
        // 使用ensure_daemon_running函数启动守护进程（这将创建独立的后台进程）
        if let Err(e) = daemon::ensure_daemon_running(socket_path) {
            tracing::error!("failed to start daemon: {}", e)
            return Err(e);
        }
    } else {
        tracing::info!("background service already running")
    }

    // 创建客户端
    let client = Client::new(socket_path);

    // 处理命令
    match cli.command {
        Commands::Mount { mount_point, data_dir } => {
            let daemon_cmd = DaemonCommand {
                name: "mount".to_string(),
                args: vec![
                    mount_point.to_string_lossy().to_string(),
                    data_dir.to_string_lossy().to_string()
                ],
            };

            match client.send_command(daemon_cmd) {
                Ok(DaemonResponse::Ok) => tracing::info!("mount command sent successfully"),
                Ok(DaemonResponse::Error(msg)) => tracing::error!("error from daemon: {}", msg),
                Ok(DaemonResponse::Data(data)) => tracing::info!("{}", data),
                Err(e) => tracing::error!("failed to send mount command: {}", e),
            }
        }
        Commands::Unmount { mount_point } => {
            let daemon_cmd = DaemonCommand {
                name: "unmount".to_string(),
                args: vec![mount_point.to_string_lossy().to_string()],
            };

            match client.send_command(daemon_cmd) {
                Ok(DaemonResponse::Ok) => tracing::info!("unmount command sent successfully"),
                Ok(DaemonResponse::Error(msg)) => tracing::error!("error from daemon: {}", msg),
                Ok(DaemonResponse::Data(data)) => tracing::info!("{}", data),
                Err(e) => tracing::error!("failed to send unmount command: {}", e),
            }
        }
        Commands::Stats { mount_point } => {
            let daemon_cmd = DaemonCommand {
                name: "stats".to_string(),
                args: vec![mount_point.to_string_lossy().to_string()],
            };

            match client.send_command(daemon_cmd) {
                Ok(DaemonResponse::Ok) => tracing::info!("stats command sent successfully"),
                Ok(DaemonResponse::Error(msg)) => tracing::error!("error from daemon: {}", msg),
                Ok(DaemonResponse::Data(data)) => tracing::info!("{}", data),
                Err(e) => tracing::error!("failed to send stats command: {}", e)
            }
        }
    }

    Ok(())
}
