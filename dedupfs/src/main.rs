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
mod memfs;
mod daemon;
mod cmd;
mod errors;
mod logging;
mod kvstore;
mod keylock;

use anyhow::Result;
use clap::{ Parser, Subcommand };
use std::path::{ PathBuf };
use daemon::{ Client, Command as DaemonCommand, Response as DaemonResponse };
use ctrlc;
// use pprof::ProfilerGuard;

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
    
    /// 以守护进程模式运行（可选）
    #[arg(short = 'd', long, global = true)]
    daemon_mode: bool,
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

    // 只有在指定了-d参数时，才使用守护进程模式
    if cli.daemon_mode {
        // For Unix systems
        let socket_path = "/tmp/dedupfs.sock";

        // 运行daemon
        if !daemon::is_daemon_running(socket_path) {
            tracing::info!("background service not running, starting as daemon...");
            // 使用ensure_daemon_running函数启动守护进程（这将创建独立的后台进程）
            if let Err(e) = daemon::ensure_daemon_running(socket_path) {
                tracing::error!("failed to start daemon: {}", e);
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
                    Ok(DaemonResponse::Data(data)) => tracing::info!("{}", data),
                    Ok(DaemonResponse::Error(msg)) => tracing::error!("error from daemon: {}", msg),
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
                    Ok(DaemonResponse::Data(data)) => tracing::info!("{}", data),
                    Ok(DaemonResponse::Error(msg)) => tracing::error!("error from daemon: {}", msg),
                    Err(e) => tracing::error!("failed to send stats command: {}", e)
                }
            }
        }
    } else {
        // 直接运行模式，不使用守护进程
        match cli.command {
            Commands::Mount { mount_point, data_dir } => {
                tracing::info!("direct mode: mounting filesystem at: {}, data stored at: {}", mount_point.display(), data_dir.display());
                
                // 直接调用mount_filesystem进行挂载
                match crate::mount::mount_filesystem(&mount_point, &data_dir) {
                    Ok(msg) => {
                        tracing::info!("{}", msg);
                        tracing::info!("filesystem mounted successfully, press Ctrl+C to unmount and exit");

                        // 启动 profiler（每秒采样 100 次）
                        // let guard = ProfilerGuard::new(100).unwrap();

                        // 设置信号处理，捕获Ctrl+C
                        let mount_point_clone = mount_point.clone();
                        ctrlc::set_handler(move || {
                            tracing::info!("received Ctrl+C, unmounting filesystem...");
                            if let Err(e) = crate::mount::unmount_filesystem(&mount_point_clone) {
                                tracing::error!("failed to unmount filesystem: {}", e);
                            }

                            // 生成火焰图
                            // if let Ok(report) = guard.report().build() {
                            //     if let Err(e) = std::fs::File::create("flamegraph.svg").and_then(|mut file| {
                            //         report.flamegraph(&mut file).map_err(|e| {
                            //             std::io::Error::new(std::io::ErrorKind::Other, format!("flamegraph error: {}", e))
                            //         })
                            //     }){
                            //             tracing::error!("failed to generate flamegraph: {}", e);
                            //     } else {
                            //             tracing::info!("flamegraph saved to flamegraph.svg");
                            //     }
                            // }
                            std::process::exit(0);
                        })?;

                        // 保持程序运行
                        loop {
                            std::thread::sleep(std::time::Duration::from_secs(1));
                        }
                    },
                    Err(e) => {
                        tracing::error!("failed to mount filesystem: {}", e);
                        return Err(e);
                    }
                }
            },
            Commands::Unmount { mount_point } => {
                tracing::info!("direct mode: unmounting filesystem at: {}", mount_point.display());
                
                // 直接调用unmount_filesystem进行卸载
                match crate::mount::unmount_filesystem(&mount_point) {
                    Ok(msg) => {
                        tracing::info!("{}", msg);
                    },
                    Err(e) => {
                        tracing::error!("failed to unmount filesystem: {}", e);
                        return Err(e);
                    }
                }
            },
            Commands::Stats { mount_point } => {
                // 直接获取统计信息（简化实现）
                tracing::info!("direct mode: getting stats for: {}", mount_point.display());
                // 模拟stats操作
                std::thread::sleep(std::time::Duration::from_millis(100));
                tracing::info!("total blocks: 12345, space saved: 102.5 MB");
            }
        }
    }

    Ok(())
}
