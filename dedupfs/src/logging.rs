use tracing_subscriber::fmt::format::{ FormatEvent, FormatFields };
use tracing_subscriber::registry::LookupSpan;
use chrono::Utc;

pub struct LogFormat;

impl<S, N> FormatEvent<S, N>
    for LogFormat
    where S: tracing::Subscriber + for<'a> LookupSpan<'a>, N: for<'a> FormatFields<'a> + 'static
{
    fn format_event(
        &self,
        ctx: &tracing_subscriber::fmt::FmtContext<'_, S, N>,
        mut writer: tracing_subscriber::fmt::format::Writer<'_>,
        event: &tracing::Event<'_>
    ) -> std::fmt::Result {
        // 写入级别
        let level = *event.metadata().level();
        let level_str = match level {
            tracing::Level::ERROR => "\x1b[31mERROR\x1b[0m", // 红色
            tracing::Level::WARN => "\x1b[33mWARN\x1b[0m", // 黄色
            tracing::Level::INFO => "\x1b[32mINFO\x1b[0m", // 绿色
            tracing::Level::DEBUG => "\x1b[36mDEBUG\x1b[0m", // 青色
            tracing::Level::TRACE => "\x1b[35mTRACE\x1b[0m", // 紫色
        };
        write!(writer, "{} ", level_str)?;

        // 写入时间戳，用方括号包围
        let now = Utc::now();
        write!(&mut writer, "[{}] ", now.format("%Y-%m-%dT%H:%M:%S"))?;

        // 写入文件名和行号
        if let Some(file) = event.metadata().file() {
            write!(&mut writer, "{}", file)?;
            if let Some(line) = event.metadata().line() {
                write!(&mut writer, ":{}", line)?;
            }
            write!(&mut writer, " ")?;
        }

        // 写入事件的消息和字段
        ctx.format_fields(writer.by_ref(), event)?;

        // 换行
        writeln!(writer)
    }
}

pub fn init ( level : u8 ) {
    // 根据verbose参数设置日志级别
    let log_level = match level {
        0 => "error", // 缺省为error级别
        1 => "warn", // -v 为warning级别
        2 => "info", // -vv 为info级别
        3 => "debug", // -vvv 为debug级别
        _ => "trace", // -vvvv及以上为trace级别
    };
    // 构建 EnvFilter：优先读 RUST_LOG，否则用 log_level
    let filter = tracing_subscriber::EnvFilter
        ::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new(log_level));
    // 初始化 tracing 日志
    tracing_subscriber::fmt().with_env_filter(filter).event_format(LogFormat).init();
}