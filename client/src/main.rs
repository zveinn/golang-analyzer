use std::env;
use std::io::Write;
use std::net::TcpStream;
use std::process;

fn main() {
    let args: Vec<String> = env::args().collect();

    if args.len() != 3 {
        eprintln!("Usage: {} <file> <line>", args[0]);
        eprintln!("Example: {} src/main.rs 42", args[0]);
        process::exit(1);
    }

    let file = &args[1];
    let line = &args[2];

    // Format exactly as the analyzer expects: "file:line"
    let message = format!("{}:{}\n", file, line);

    // Connect to the analyzer's TCP trigger port.
    // Can be overridden with ANALYZER_ADDR env var.
    let addr = env::var("ANALYZER_ADDR").unwrap_or_else(|_| "127.0.0.1:2222".to_string());

    match TcpStream::connect(&addr) {
        Ok(mut stream) => {
            if let Err(e) = stream.write_all(message.as_bytes()) {
                eprintln!("Failed to send trigger: {}", e);
                process::exit(1);
            }
            // Best effort: close the write side so the server sees EOF
            let _ = stream.shutdown(std::net::Shutdown::Write);
        }
        Err(e) => {
            eprintln!("Could not connect to analyzer at {}: {}", addr, e);
            eprintln!("Is the code-analyzer server running? (set ANALYZER_ADDR to override)");
            process::exit(1);
        }
    }
}
