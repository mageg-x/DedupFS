// daemonize.rs (修复版本)
use anyhow::{ Context, Result };
use serde::{ Deserialize, Serialize };
use postcard::{from_bytes, to_stdvec};
use tracing;
use std::collections::HashMap;
use std::sync::{ Arc, RwLock };
use tokio::io::{ AsyncReadExt, AsyncWriteExt };
use crate::cmd;
use crate::logging;

use std::os::unix::io::AsRawFd;
use std::io::{ Read, Write };

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct Command {
    pub name: String,
    pub args: Vec<String>,
}

#[derive(Serialize, Deserialize, Debug)]
pub enum Response {
    Ok,
    Error(String),
    Data(String),
}

pub struct Daemon {
    handlers: Arc<
        RwLock<HashMap<String, Box<dyn (Fn(Command) -> Result<Response>) + Send + Sync>>>
    >,
    socket_path: String,
}

impl Daemon {
    pub fn new(socket_path: &str) -> Self {
        Self {
            handlers: Arc::new(RwLock::new(HashMap::new())),
            socket_path: socket_path.to_string(),
        }
    }

    pub fn register_handler<F>(&mut self, command_name: &str, handler: F)
        where F: Fn(Command) -> Result<Response> + Send + Sync + 'static
    {
        let mut handlers = self.handlers.write().unwrap();
        handlers.insert(command_name.to_string(), Box::new(handler));
    }

    pub async fn start(&self) -> Result<()> {
        // 清理可能存在的旧socket文件
        let _ = std::fs::remove_file(&self.socket_path);

        let listener = tokio::net::UnixListener::bind(&self.socket_path).map_err(|e|
            crate::errors::new(crate::errors::SocketBindError {
                path: self.socket_path.clone(),
            })
        )?;
        tracing::info!("daemon listening on uds: {}", self.socket_path);

        loop {
            let (stream, _) = listener.accept().await?;
            let handlers = self.handlers.clone();
            tokio::spawn(async move {
                if let Err(e) = handle_client_unix(stream, handlers).await {
                    tracing::error!("client error: {}", e);
                }
            });
        }
    }
}

async fn handle_client_unix(
    mut stream: tokio::net::UnixStream,
    handlers: Arc<RwLock<HashMap<String, Box<dyn (Fn(Command) -> Result<Response>) + Send + Sync>>>>
) -> Result<()> {
    let mut length_bytes = [0u8; 4];
    stream.read_exact(&mut length_bytes).await.map_err(|e|
        crate::errors::new(crate::errors::SocketCommunicationError {
            error: e.to_string(),
        })
    )?;
    let length = u32::from_le_bytes(length_bytes) as usize;

    let mut buffer = vec![0u8; length];
    stream.read_exact(&mut buffer).await.map_err(|e|
        crate::errors::new(crate::errors::SocketCommunicationError {
            error: e.to_string(),
        })
    )?;

    let command: Command = from_bytes(&buffer).map_err(|e|
        crate::errors::new(crate::errors::DeserializationError {
            error: e.to_string(),
        })
    )?;

    let response = {
        let handlers = handlers.read().unwrap();
        if let Some(handler) = handlers.get(&command.name) {
            handler(command)
        } else {
            Err(
                crate::errors::new(crate::errors::CommandNotRegistered {
                    name: command.name.clone(),
                })
            )
        }
    };

    let response = response.unwrap_or_else(|e| Response::Error(e.to_string()));
    let response_bytes = to_stdvec(&response).map_err(|e|
        crate::errors::new(crate::errors::SerializationError {
            error: e.to_string(),
        })
    )?;
    let response_length = (response_bytes.len() as u32).to_le_bytes();

    stream.write_all(&response_length).await.map_err(|e|
        crate::errors::new(crate::errors::SocketCommunicationError {
            error: e.to_string(),
        })
    )?;
    stream.write_all(&response_bytes).await.map_err(|e|
        crate::errors::new(crate::errors::SocketCommunicationError {
            error: e.to_string(),
        })
    )?;
    stream.flush().await.map_err(|e|
        crate::errors::new(crate::errors::SocketCommunicationError {
            error: e.to_string(),
        })
    )?;

    Ok(())
}

pub struct Client {
    socket_path: String,
}

impl Client {
    pub fn new(socket_path: &str) -> Self {
        Self {
            socket_path: socket_path.to_string(),
        }
    }

    pub fn send_command(&self, command: Command) -> Result<Response> {
        tracing::debug!("sending command sync: {:?}", command);
        let mut stream = std::os::unix::net::UnixStream::connect(&self.socket_path).map_err(|e|
            crate::errors::new(crate::errors::SocketConnectError {
                path: self.socket_path.clone(),
            })
        )?;

        let command_bytes = to_stdvec(&command).map_err(|e|
            crate::errors::new(crate::errors::SerializationError {
                error: e.to_string(),
            })
        )?;
        let length = (command_bytes.len() as u32).to_le_bytes();

        stream.write_all(&length).map_err(|e|
            crate::errors::new(crate::errors::SocketCommunicationError {
                error: e.to_string(),
            })
        )?;
        stream.write_all(&command_bytes).map_err(|e|
            crate::errors::new(crate::errors::SocketCommunicationError {
                error: e.to_string(),
            })
        )?;
        stream.flush().map_err(|e|
            crate::errors::new(crate::errors::SocketCommunicationError {
                error: e.to_string(),
            })
        )?;

        let mut length_bytes = [0u8; 4];
        stream.read_exact(&mut length_bytes).map_err(|e|
            crate::errors::new(crate::errors::SocketCommunicationError {
                error: e.to_string(),
            })
        )?;
        let response_length = u32::from_le_bytes(length_bytes) as usize;

        let mut buffer = vec![0u8; response_length];
        stream.read_exact(&mut buffer).map_err(|e|
            crate::errors::new(crate::errors::SocketCommunicationError {
                error: e.to_string(),
            })
        )?;

        let response: Response = from_bytes(&buffer).map_err(|e|
            crate::errors::new(crate::errors::DeserializationError {
                error: e.to_string(),
            })
        )?;
        Ok(response)
    }
}

// 工具函数：检查守护进程是否在运行
pub fn is_daemon_running(socket_path: &str) -> bool {
    let client = Client::new(socket_path);
    let test_cmd = Command {
        name: "ping".to_string(),
        args: vec![],
    };

    client.send_command(test_cmd).is_ok()
}

pub fn ensure_daemon_running(socket_path: &str) -> Result<(), anyhow::Error> {
    if is_daemon_running(socket_path) {
        return Ok(());
    }

    // ========== 直接 fork！不经过 Tokio ==========
    let pid = unsafe { libc::fork() };
    if pid < 0 {
        tracing::error!("fork failed");
        return Err(crate::errors::new(crate::errors::ForkFailed));
    }

    if pid == 0 {
        // ========== 子进程：daemon ==========

        unsafe {
            libc::setsid();
            // 可选：重定向标准流到 /dev/null
            // let devnull = std::fs::File::open("/dev/null").expect("Cannot open /dev/null");
            // let fd = devnull.as_raw_fd();
            // libc::dup2(fd, 0);
            // libc::dup2(fd, 1);
            // libc::dup2(fd, 2);
            // std::mem::forget(devnull); // 防止 close
        }

        // 子进程是干净的，现在可以安全创建 Tokio Runtime
        let rt = tokio::runtime::Runtime::new().map_err(|e|
            crate::errors::new(crate::errors::RuntimeCreationFailed {
                reason: e.to_string(),
            })
        )?;

        let result = rt.block_on(async {
            let mut daemon = Daemon::new(socket_path);
            daemon.register_handler("ping", |_| Ok(Response::Ok));
            daemon.register_handler("mount", cmd::handle_mount);
            daemon.register_handler("unmount", cmd::handle_unmount);
            daemon.register_handler("stats", cmd::handle_stats);
            daemon.start().await
        });

        match result {
            Ok(()) => std::process::exit(0),
            Err(e) => {
                tracing::error!("daemon error: {}", e);
                std::process::exit(1);
            }
        }
    }

    // ========== 父进程：等待 daemon 启动 ==========
    for _ in 0..20 {
        std::thread::sleep(std::time::Duration::from_millis(200));
        if is_daemon_running(socket_path) {
            return Ok(());
        }
    }

    Err(crate::errors::new(crate::errors::DaemonStartupTimeout))
}
