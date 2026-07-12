use clap::Parser;
use std::path::{Path, PathBuf};
use std::collections::{HashMap, HashSet};
use std::fs;
use std::sync::Arc;
use walkdir::WalkDir;
use tree_sitter::{Parser as TsParser, Node, Tree};

#[derive(Clone, Hash, Eq, PartialEq, Debug)]
struct FuncKey {
    pkg: Arc<str>,
    recv: Option<Arc<str>>,
    name: Arc<str>,
}

impl serde::Serialize for FuncKey {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        use serde::ser::SerializeStruct;
        let mut state = serializer.serialize_struct("FuncKey", 3)?;
        state.serialize_field("pkg", &*self.pkg)?;
        state.serialize_field("recv", &self.recv.as_ref().map(|s| s.as_ref()))?;
        state.serialize_field("name", &*self.name)?;
        state.end()
    }
}

impl FuncKey {
    fn display(&self) -> String {
        match &self.recv {
            Some(r) => format!("{}({}).{}", self.pkg, r, self.name),
            None => format!("{}.{}", self.pkg, self.name),
        }
    }
}

#[derive(Clone, Debug, serde::Serialize)]
enum Callee {
    Local(FuncKey),
    External(String),
    Stdlib(String),
}

#[derive(Clone, Debug, serde::Serialize)]
struct CallInfo {
    callee: Callee,
    goroutine: bool,
}

#[derive(Clone, Debug, serde::Serialize)]
#[serde(tag = "kind")]
enum BodyElement {
    Call { info: CallInfo },
    Loop { body: Vec<BodyElement> },
}



#[derive(Parser, Debug)]
#[command(name = "code-analyzer", version, about = "Analyze Go function execution chains. Run with file+line for CLI, or as server for UI.")]
struct Cli {
    /// HTTP address for UI and WebSocket
    #[arg(long, default_value = "0.0.0.0:1111")]
    addr: String,

    /// TCP address for receiving file:line triggers
    #[arg(long, default_value = "0.0.0.0:2222")]
    tcp_addr: String,

    /// Optional: Path to Go source file (for direct CLI mode)
    #[arg(value_name = "FILE")]
    file: Option<PathBuf>,

    /// Optional: 1-based line number (for direct CLI mode)
    #[arg(value_name = "LINE")]
    line: Option<usize>,
}

fn main() {
    let cli = Cli::parse();

    if let (Some(file), Some(line)) = (&cli.file, cli.line) {
        // Direct CLI mode (original behavior)
        if let Err(e) = run(file, line) {
            eprintln!("error: {e}");
            std::process::exit(1);
        }
        return;
    }

    // Server mode - run the async server
    if let Err(e) = tokio::runtime::Runtime::new()
        .unwrap()
        .block_on(run_server(&cli.addr, &cli.tcp_addr))
    {
        eprintln!("server error: {e}");
        std::process::exit(1);
    }
}

#[derive(serde::Serialize, Clone)]
struct AnalysisPayload {
    root: FuncKey,
    body: Vec<BodyElement>,
    graph: Vec<(FuncKey, Vec<BodyElement>)>,
}

fn perform_analysis(file: &Path, line: usize) -> Result<AnalysisPayload, String> {
    let abs_file = if file.is_absolute() {
        file.to_path_buf()
    } else {
        std::env::current_dir().map_err(|e| e.to_string())?.join(file)
    };

    let (module_root, module_name) = find_go_module(&abs_file)
        .ok_or_else(|| "Could not find go.mod".to_string())?;

    let (graph, defs) = build_call_graph(&module_root, &module_name)
        .map_err(|e| e.to_string())?;

    let mut start_callee = resolve_call_at_line(&abs_file, line, &module_root, &module_name, &defs);

    if start_callee.is_none() {
        // Fallback: if the line is inside a known local function, analyze from that function definition.
        if let Some(key) = find_enclosing_function_key(&abs_file, line, &module_root, &module_name, &defs) {
            start_callee = Some(Callee::Local(key));
        }
    }

    let start_callee = start_callee.ok_or_else(|| "Could not locate a resolvable call or enclosing function at the given line.".to_string())?;

    match start_callee {
        Callee::Local(key) => {
            let body = graph.get(&key).cloned().unwrap_or_default();
            let graph_list: Vec<_> = graph.into_iter().collect();
            Ok(AnalysisPayload { root: key, body, graph: graph_list })
        }
        Callee::External(name) | Callee::Stdlib(name) => {
            let fake_key = FuncKey {
                pkg: Arc::from("external"),
                recv: None,
                name: Arc::from(name.as_str()),
            };
            Ok(AnalysisPayload {
                root: fake_key,
                body: vec![],
                graph: vec![],
            })
        }
    }
}

fn run(file: &Path, line: usize) -> Result<(), Box<dyn std::error::Error>> {
    let abs_file = if file.is_absolute() {
        file.to_path_buf()
    } else {
        std::env::current_dir()?.join(file)
    };

    let (module_root, module_name) = match find_go_module(&abs_file) {
        Some(v) => v,
        None => {
            return Err("Could not find go.mod. Make sure the file is inside a Go module.".into());
        }
    };

    println!("{} {}", color_loc("Module:"), module_name);
    println!("{} {}:{}", color_loc("Analyzing call at"), abs_file.display(), line);

    let (graph, defs) = build_call_graph(&module_root, &module_name)?;

    // Locate the starting callee from the call site
    let mut start_callee = resolve_call_at_line(&abs_file, line, &module_root, &module_name, &defs);

    if start_callee.is_none() {
        // Fallback: if the line is inside a known local function, analyze from that function definition.
        if let Some(key) = find_enclosing_function_key(&abs_file, line, &module_root, &module_name, &defs) {
            start_callee = Some(Callee::Local(key));
        }
    }

    let start_callee = match start_callee {
        Some(c) => c,
        None => {
            eprintln!("Could not locate a resolvable call or enclosing function at the given line.");
            return Ok(());
        }
    };

    match &start_callee {
        Callee::Local(key) => {
            println!("\n{}", paint("Execution chain starting from:", "1"));
            let mut loop_counter = 0usize;
            print_execution_tree(key, &graph, &defs, 0, &mut HashSet::new(), &mut HashSet::new(), &mut loop_counter, 20);
        }
        Callee::External(name) => {
            println!("\n{}", paint("Call at given location is to external (not following):", "1"));
            println!("  {}", color_external(name));
        }
        Callee::Stdlib(name) => {
            println!("\n{}", paint("Call at given location is to the standard library (not following):", "1"));
            println!("  {}", color_external(&format!("[stdlib] {}", name)));  // label it clearly
        }
    }

    Ok(())
}

use axum::{
    extract::{
        ws::{Message, WebSocket, WebSocketUpgrade},
        State,
    },
    response::IntoResponse,
    routing::get,
    Router,
};
use rust_embed::RustEmbed;
use tokio::sync::broadcast;

#[derive(RustEmbed)]
#[folder = "ui/dist"]
struct Assets;

async fn serve_embedded(path: &str) -> impl IntoResponse {
    let path = if path.is_empty() { "index.html" } else { path };
    match Assets::get(path) {
        Some(content) => {
            let mime = mime_guess::from_path(path).first_or_octet_stream();
            ([("content-type", mime.to_string())], content.data).into_response()
        }
        None => {
            // Fallback to index for SPA
            if let Some(content) = Assets::get("index.html") {
                ([("content-type", "text/html".to_string())], content.data).into_response()
            } else {
                (axum::http::StatusCode::NOT_FOUND, "Not found").into_response()
            }
        }
    }
}

#[derive(Clone)]
struct AppState {
    tx: broadcast::Sender<String>, // JSON of AnalysisPayload
}

async fn ws_handler(ws: WebSocketUpgrade, State(state): State<AppState>) -> impl IntoResponse {
    ws.on_upgrade(|socket| handle_socket(socket, state))
}

async fn handle_socket(mut socket: WebSocket, state: AppState) {
    let mut rx = state.tx.subscribe();

    // Send current if any? For now, client will get next update.
    // You can store latest in state if wanted.

    loop {
        tokio::select! {
            msg = rx.recv() => {
                if let Ok(json) = msg {
                    if socket.send(Message::Text(json)).await.is_err() {
                        break;
                    }
                }
            }
            Some(msg) = socket.recv() => {
                if msg.is_err() {
                    break;
                }
            }
            else => break,
        }
    }
}

async fn run_server(http_addr: &str, tcp_addr: &str) -> Result<(), Box<dyn std::error::Error>> {
    println!("Starting server...");
    println!("  HTTP UI + WS: http://{}", http_addr);
    println!("  TCP trigger : {}", tcp_addr);
    println!("Send file:line over TCP to trigger analysis (e.g. echo 'main.go:145' | nc localhost 2222)");

    let (tx, _rx) = broadcast::channel::<String>(16);
    let state = AppState { tx: tx.clone() };

    // HTTP router
    let app = Router::new()
        .route("/ws", get(ws_handler))
        .route("/*path", get(|axum::extract::Path(path): axum::extract::Path<String>| async move {
            serve_embedded(&path).await
        }))
        .route("/", get(|| async { serve_embedded("").await }))
        .with_state(state.clone());

    let http_addr = http_addr.to_string();
    let tcp_addr = tcp_addr.to_string();
    let tx_clone = tx.clone();

    // Spawn TCP listener
    tokio::spawn(async move {
        if let Err(e) = run_tcp_listener(&tcp_addr, tx_clone).await {
            eprintln!("TCP listener error: {}", e);
        }
    });

    // Run HTTP server
    let listener = tokio::net::TcpListener::bind(&http_addr).await?;
    println!("HTTP server listening on {}", http_addr);
    axum::serve(listener, app).await?;

    Ok(())
}

async fn run_tcp_listener(addr: &str, tx: broadcast::Sender<String>) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    println!("TCP listener listening on {}", addr);

    loop {
        let (socket, _) = listener.accept().await?;
        let tx = tx.clone();

        tokio::spawn(async move {
            use tokio::io::{AsyncBufReadExt, BufReader};
            let mut reader = BufReader::new(socket);
            let mut line = String::new();

            loop {
                line.clear();
                match reader.read_line(&mut line).await {
                    Ok(0) => break,
                    Ok(_) => {
                        let data = line.trim().to_string();
                        if data.is_empty() { continue; }

                        if let Some((file_str, line_str)) = data.rsplit_once(':') {
                            if let Ok(line) = line_str.trim().parse::<usize>() {
                                let path = std::path::PathBuf::from(file_str.trim());
                                println!("Received trigger: {}:{}", path.display(), line);

                                match perform_analysis(&path, line) {
                                    Ok(payload) => {
                                        if let Ok(json) = serde_json::to_string(&payload) {
                                            let _ = tx.send(json);
                                        }
                                    }
                                    Err(e) => {
                                        eprintln!("Analysis error: {}", e);
                                    }
                                }
                            }
                        }
                    }
                    Err(_) => break,
                }
            }
        });
    }
}

/// Walk up from the given file to find go.mod.
/// Returns (module_root, module_name)
fn find_go_module(start_file: &Path) -> Option<(PathBuf, String)> {
    let mut current = if start_file.is_file() {
        start_file.parent()?.to_path_buf()
    } else {
        start_file.to_path_buf()
    };

    loop {
        let go_mod = current.join("go.mod");
        if go_mod.exists() {
            if let Ok(content) = fs::read_to_string(&go_mod) {
                for line in content.lines() {
                    let line = line.trim();
                    if let Some(rest) = line.strip_prefix("module ") {
                        let name = rest.trim().to_string();
                        if !name.is_empty() {
                            return Some((current, name));
                        }
                    }
                }
            }
            // fallback if no module name parsed
            let name = current.file_name()?.to_string_lossy().to_string();
            return Some((current, name));
        }

        if !current.pop() {
            break;
        }
    }
    None
}

/// Collect all .go files under root, skipping vendor, .git, and common generated dirs.
/// Returns absolute paths.
fn collect_go_files(root: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    for entry in WalkDir::new(root)
        .follow_links(false)
        .into_iter()
        .filter_entry(|e| {
            let name = e.file_name().to_string_lossy();
            // Skip heavy/irrelevant directories
            if name == "vendor" || name == ".git" || name == "testdata" || name == "node_modules" {
                return false;
            }
            // Skip hidden dirs except root
            if e.depth() > 0 && name.starts_with('.') {
                return false;
            }
            true
        })
    {
        if let Ok(e) = entry {
            if e.file_type().is_file() {
                let p = e.path();
                if let Some(ext) = p.extension() {
                    if ext == "go" {
                        // Optionally skip _test.go for "execution" analysis? Keep for now.
                        files.push(p.to_path_buf());
                    }
                }
            }
        }
    }
    files
}

// ============================================================================
// Core analysis
// ============================================================================

type CallGraph = HashMap<FuncKey, Vec<BodyElement>>;
type DefMap = HashMap<FuncKey, (PathBuf, usize)>;

/// Build the call graph and definition locations.
/// Two passes over files (very cheap) so we can use full method name info for resolution.
fn build_call_graph(module_root: &Path, module_name: &str) -> Result<(CallGraph, DefMap), Box<dyn std::error::Error>> {
    let go_files = collect_go_files(module_root);

    let mut parser = TsParser::new();
    parser.set_language(&tree_sitter_go::language())
        .expect("failed to load tree-sitter-go grammar");

    let mut graph: CallGraph = HashMap::new();
    let mut defs: DefMap = HashMap::new();
    let mut method_index: HashMap<String, Vec<FuncKey>> = HashMap::new();

    // === PASS 1: Collect all definitions and method names (memory efficient - one file at a time) ===
    for file_path in &go_files {
        let source = match fs::read_to_string(file_path) {
            Ok(s) => s,
            Err(_) => continue,
        };
        if source.trim().is_empty() { continue; }

        let tree = match parser.parse(source.as_bytes(), None) {
            Some(t) => t,
            None => continue,
        };

        let pkg = compute_pkg_path(file_path, module_root, module_name);
        let func_infos = extract_function_infos(&tree, &source, &pkg, file_path);

        for info in &func_infos {
            defs.entry(info.key.clone()).or_insert((info.file.clone(), info.line));
            graph.entry(info.key.clone()).or_insert_with(Vec::new);

            if info.key.recv.is_some() {
                method_index
                    .entry(info.key.name.to_string())
                    .or_default()
                    .push(info.key.clone());
            }
        }

        drop(tree);
        drop(source);
    }

    // === PASS 2: Extract calls, using method_index for better resolution of methods ===
    for file_path in &go_files {
        let source = match fs::read_to_string(file_path) {
            Ok(s) => s,
            Err(_) => continue,
        };
        if source.trim().is_empty() { continue; }

        let tree = match parser.parse(source.as_bytes(), None) {
            Some(t) => t,
            None => continue,
        };

        let pkg = compute_pkg_path(file_path, module_root, module_name);
        let imports = extract_imports(&tree, &source);

        // Structured extraction: visit the tree, and for each function/method declaration
        // extract its body as nested elements (loops contain their calls).
        let root = tree.root_node();
        populate_body_elements(
            root,
            &source,
            &pkg,
            &imports,
            module_name,
            &method_index,
            &mut graph,
        );

        drop(tree);
        drop(source);
    }

    // Post-process:
    // - Convert any Local that has no definition (e.g. func vars like doCMD, or type conversions) into External
    //   so we still surface them (especially important for goroutine annotations).
    for body in graph.values_mut() {
        clean_body_elements(body, &defs);
    }

    Ok((graph, defs))
}

struct FuncInfo {
    key: FuncKey,
    start_byte: usize,
    end_byte: usize,
    line: usize,
    file: PathBuf,
}

fn compute_pkg_path(file: &Path, root: &Path, module_name: &str) -> String {
    if let Ok(rel) = file.strip_prefix(root) {
        let parent = rel.parent().unwrap_or(Path::new(""));
        let p = parent.to_string_lossy().replace('\\', "/");
        if p.is_empty() || p == "." {
            return module_name.to_string();
        }
        return format!("{}/{}", module_name, p);
    }
    module_name.to_string()
}

fn extract_imports(tree: &Tree, source: &str) -> HashMap<String, String> {
    let mut imports = HashMap::new();

    // Simple tree walk for import specs (robust and low overhead)
    let _cursor = tree.walk();
    fn walk_imports(node: Node, source: &str, imports: &mut HashMap<String, String>) {
        if node.kind() == "import_spec" {
            let mut path: Option<String> = None;
            let mut name: Option<String> = None;

            for i in 0..node.child_count() {
                if let Some(child) = node.child(i) {
                    match child.kind() {
                        "interpreted_string_literal" | "raw_string_literal" => {
                            let text = child.utf8_text(source.as_bytes()).unwrap_or("");
                            path = Some(strip_quotes(text));
                        }
                        "package_identifier" | "dot" | "blank_identifier" => {
                            // alias or . or _
                            let t = child.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                            if t != "." && t != "_" {
                                name = Some(t);
                            } else if t == "." {
                                // dot import: rare, we can handle specially later
                                name = Some(".".to_string());
                            }
                        }
                        _ => {}
                    }
                }
            }

            if let Some(p) = path {
                let alias = name.unwrap_or_else(|| {
                    // default alias is last segment of path
                    p.rsplit('/').next().unwrap_or(&p).to_string()
                });
                imports.insert(alias, p);
            }
        }

        let mut c = node.walk();
        for child in node.children(&mut c) {
            walk_imports(child, source, imports);
        }
    }

    walk_imports(tree.root_node(), source, &mut imports);
    imports
}

fn strip_quotes(s: &str) -> String {
    let s = s.trim();
    if (s.starts_with('"') && s.ends_with('"')) || (s.starts_with('`') && s.ends_with('`')) {
        s[1..s.len()-1].to_string()
    } else {
        s.to_string()
    }
}

/// Extract named functions and methods + their body byte ranges.
fn extract_function_infos(tree: &Tree, source: &str, pkg: &str, file: &Path) -> Vec<FuncInfo> {
    let mut infos = Vec::new();
    let root = tree.root_node();

    // We walk and look for function_declaration and method_declaration
    let _cursor = root.walk();
    fn visit(node: Node, source: &str, pkg: &str, file: &Path, infos: &mut Vec<FuncInfo>) {
        match node.kind() {
            "function_declaration" => {
                if let Some(name_node) = node.child_by_field_name("name") {
                    let name = name_node.utf8_text(source.as_bytes()).unwrap_or("??").to_string();
                    if let Some(body) = node.child_by_field_name("body") {
                        let start = body.start_byte();
                        let end = body.end_byte();
                        let line = name_node.start_position().row + 1;
                        let key = FuncKey {
                            pkg: Arc::from(pkg),
                            recv: None,
                            name: Arc::from(name.as_str()),
                        };
                        infos.push(FuncInfo {
                            key,
                            start_byte: start,
                            end_byte: end,
                            line,
                            file: file.to_path_buf(),
                        });
                    }
                }
            }
            "method_declaration" => {
                if let Some(name_node) = node.child_by_field_name("name") {
                    let name = name_node.utf8_text(source.as_bytes()).unwrap_or("??").to_string();
                    let recv = extract_receiver_type(node, source);
                    if let Some(body) = node.child_by_field_name("body") {
                        let start = body.start_byte();
                        let end = body.end_byte();
                        let line = name_node.start_position().row + 1;
                        let key = FuncKey {
                            pkg: Arc::from(pkg),
                            recv: recv.map(|r| Arc::from(r.as_str())),
                            name: Arc::from(name.as_str()),
                        };
                        infos.push(FuncInfo {
                            key,
                            start_byte: start,
                            end_byte: end,
                            line,
                            file: file.to_path_buf(),
                        });
                    }
                }
            }
            _ => {}
        }

        let mut c = node.walk();
        for child in node.children(&mut c) {
            visit(child, source, pkg, file, infos);
        }
    }

    visit(root, source, pkg, file, &mut infos);
    infos
}

fn extract_receiver_type(method_node: Node, source: &str) -> Option<String> {
    // receiver is under parameter_list -> parameter_declaration -> type
    let recv_list = method_node.child_by_field_name("receiver")?;
    for i in 0..recv_list.child_count() {
        if let Some(param_decl) = recv_list.child(i) {
            if param_decl.kind() == "parameter_declaration" {
                // The type can be pointer_type or type_identifier or qualified_type
                for j in 0..param_decl.child_count() {
                    if let Some(tnode) = param_decl.child(j) {
                        if tnode.kind().ends_with("type") || tnode.kind() == "pointer_type" || tnode.kind() == "type_identifier" || tnode.kind() == "qualified_type" {
                            let mut text = tnode.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                            // Normalize a bit: remove extra parens etc if any
                            text = text.trim().to_string();
                            if text.starts_with('(') && text.ends_with(')') {
                                text = text[1..text.len()-1].trim().to_string();
                            }
                            if !text.is_empty() {
                                return Some(text);
                            }
                        }
                    }
                }
            }
        }
    }
    None
}

/// Find calls and resolve them (one call site may resolve to multiple possible callees for methods).
/// Tracks whether each call happens inside a loop or as a goroutine launch.
fn extract_resolved_calls(
    tree: &Tree,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    module_name: &str,
    method_index: &HashMap<String, Vec<FuncKey>>,
) -> Vec<(usize, Vec<CallInfo>)> {
    let mut results = Vec::new();
    let root = tree.root_node();

    fn visit(
        node: Node,
        source: &str,
        current_pkg: &str,
        imports: &HashMap<String, String>,
        module_name: &str,
        method_index: &HashMap<String, Vec<FuncKey>>,
        in_goroutine: bool,
        out: &mut Vec<(usize, Vec<CallInfo>)>,
    ) {
        let now_in_goroutine = in_goroutine || node.kind() == "go_statement";

        if node.kind() == "call_expression" {
            if let Some(func_node) = node.child_by_field_name("function") {
                let callees = resolve_callee_node(func_node, source, current_pkg, imports, module_name, method_index);
                if !callees.is_empty() {
                    let infos: Vec<CallInfo> = callees
                        .into_iter()
                        .map(|c| CallInfo {
                            callee: c,
                            goroutine: now_in_goroutine,
                        })
                        .collect();
                    out.push((node.start_byte(), infos));
                }
            }
        }

        let mut c = node.walk();
        for child in node.children(&mut c) {
            visit(child, source, current_pkg, imports, module_name, method_index, now_in_goroutine, out);
        }
    }

    visit(root, source, current_pkg, imports, module_name, method_index, false, &mut results);
    results
}

/// Resolve a "function" node of a call_expression.
/// Can return multiple for method name ambiguity (all possible targets).
fn resolve_callee_node(
    func_node: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    module_name: &str,
    method_index: &HashMap<String, Vec<FuncKey>>,
) -> Vec<Callee> {
    match func_node.kind() {
        "identifier" => {
            let name = func_node.utf8_text(source.as_bytes()).unwrap_or("").to_string();
            if name.is_empty() || is_builtin(&name) {
                return vec![];
            }
            let key = FuncKey {
                pkg: Arc::from(current_pkg),
                recv: None,
                name: Arc::from(name.as_str()),
            };
            vec![Callee::Local(key)]
        }
        "selector_expression" => {
            let field = match func_node.child_by_field_name("field") { Some(f) => f, None => return vec![] };
            let operand = match func_node.child_by_field_name("operand") { Some(o) => o, None => return vec![] };

            let called_name = field.utf8_text(source.as_bytes()).unwrap_or("").to_string();
            let operand_text = operand.utf8_text(source.as_bytes()).unwrap_or("").to_string();

            // Package-qualified call: pkg.Func()
            if let Some(full_import) = imports.get(&operand_text) {
                if is_standard_library(full_import) {
                    let desc = format!("{}.{}", full_import, called_name);
                    return vec![Callee::Stdlib(desc)];
                }
                if full_import.starts_with(module_name) {
                    let key = FuncKey {
                        pkg: Arc::from(full_import.as_str()),
                        recv: None,
                        name: Arc::from(called_name.as_str()),
                    };
                    return vec![Callee::Local(key)];
                } else {
                    let ext = format!("{}.{}", full_import, called_name);
                    return vec![Callee::External(ext)];
                }
            }

            // Rare pkg.Type form
            if operand.kind() == "selector_expression" {
                if let (Some(op_field), Some(op_op)) = (operand.child_by_field_name("field"), operand.child_by_field_name("operand")) {
                    let op_op_text = op_op.utf8_text(source.as_bytes()).unwrap_or("");
                    if let Some(full) = imports.get(op_op_text) {
                        if is_standard_library(full) {
                            let desc = format!("{}.{}.{}", full, op_field.utf8_text(source.as_bytes()).unwrap_or(""), called_name);
                            return vec![Callee::Stdlib(desc)];
                        }
                        if full.starts_with(module_name) {
                            let ext = format!("{}.{}.{}", full, op_field.utf8_text(source.as_bytes()).unwrap_or(""), called_name);
                            return vec![Callee::External(ext)];
                        }
                    }
                }
            }

            // Method call on a value (e.g. c.Method() or obj.Field()).
            // Prefer local methods resolved by name. Only fall back to a descriptive
            // External/Stdlib label if we couldn't match any local method definition.
            // This prevents internal struct methods (like c.NewSessionForCommand on *CMD)
            // from being mislabeled as [external].
            let mut out = vec![];

            if let Some(cands) = method_index.get(&called_name) {
                for c in cands {
                    out.push(Callee::Local(c.clone()));
                }
            }

            if out.is_empty() {
                // No local methods found for this name → use descriptive form
                let raw = format!("{}.{}", operand_text, called_name);
                if let Some(full_import) = imports.get(&operand_text) {
                    if is_standard_library(full_import) {
                        let desc = format!("{}.{}", full_import, called_name);
                        out.push(Callee::Stdlib(desc));
                    } else {
                        out.push(Callee::External(raw));
                    }
                } else {
                    out.push(Callee::External(raw));
                }
            }

            out
        }
        "parenthesized_expression" => {
            if let Some(inner) = func_node.child(0) {
                return resolve_callee_node(inner, source, current_pkg, imports, module_name, method_index);
            }
            vec![]
        }
        _ => vec![],
    }
}

fn is_builtin(name: &str) -> bool {
    matches!(name, "append" | "cap" | "close" | "complex" | "copy" | "delete" | "imag" | "len" | "make" | "new" | "panic" | "print" | "println" | "real" | "recover")
}

fn is_standard_library(import_path: &str) -> bool {
    if import_path == "C" {
        return true;
    }
    let first = import_path.split('/').next().unwrap_or(import_path);
    !first.contains('.')
}

fn find_enclosing_func<'a>(infos: &'a [FuncInfo], byte_pos: usize) -> Option<&'a FuncInfo> {
    // Return the tightest (smallest range) that contains byte_pos
    let mut best: Option<&FuncInfo> = None;
    let mut best_size = usize::MAX;
    for info in infos {
        if byte_pos >= info.start_byte && byte_pos < info.end_byte {
            let size = info.end_byte - info.start_byte;
            if size < best_size {
                best_size = size;
                best = Some(info);
            }
        }
    }
    best
}

fn callee_matches(a: &Callee, b: &Callee) -> bool {
    match (a, b) {
        (Callee::Local(ka), Callee::Local(kb)) => ka == kb,
        (Callee::External(sa), Callee::External(sb)) => sa == sb,
        (Callee::Stdlib(sa), Callee::Stdlib(sb)) => sa == sb,
        _ => false,
    }
}

// ---------------------------------------------------------------------------
// Simple ANSI coloring (no extra deps). Respects NO_COLOR.
// ---------------------------------------------------------------------------

fn color_enabled() -> bool {
    std::env::var_os("NO_COLOR").is_none()
}

fn paint(text: &str, code: &str) -> String {
    if color_enabled() {
        format!("\x1b[{}m{}\x1b[0m", code, text)
    } else {
        text.to_string()
    }
}

fn color_func(text: &str) -> String {
    paint(text, "1;36") // bold cyan
}

fn color_method(text: &str) -> String {
    paint(text, "1;36")
}

fn color_goroutine(text: &str) -> String {
    paint(text, "1;35") // bold magenta
}

fn color_loop(text: &str) -> String {
    paint(text, "1;33") // bold yellow
}

fn color_external(text: &str) -> String {
    paint(text, "2;37") // dim
}

fn color_loc(text: &str) -> String {
    paint(text, "2") // dim
}

fn format_annotations(in_loop: bool, _goroutine: bool) -> String {
    // Note: in_loop is no longer used for tags (loops are now structural).
    // Goroutine uses "go " prefix instead.
    if in_loop {
        format!(" [{}]", color_loop("in loop"))
    } else {
        String::new()
    }
}

/// Recursively convert unresolved Local callees to short External (or leave as-is) names inside BodyElements.
fn clean_body_elements(elements: &mut [BodyElement], defs: &DefMap) {
    for elem in elements.iter_mut() {
        match elem {
            BodyElement::Call { info: ci } => {
                if let Callee::Local(k) = &ci.callee {
                    if !defs.contains_key(k) {
                        let short = if let Some(r) = &k.recv {
                            format!("({}).{}", r, k.name)
                        } else {
                            k.name.to_string()
                        };
                        ci.callee = Callee::External(short);
                    }
                }
            }
            BodyElement::Loop { body } => {
                clean_body_elements(body, defs);
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Structured body extraction (loops as first-class, calls placed inside them)
// ---------------------------------------------------------------------------

fn extract_body_elements(
    body: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    module_name: &str,
    method_index: &HashMap<String, Vec<FuncKey>>,
) -> Vec<BodyElement> {
    let mut elems = Vec::new();
    let mut cursor = body.walk();
    for child in body.children(&mut cursor) {
        process_node_for_body(
            child,
            source,
            current_pkg,
            imports,
            module_name,
            method_index,
            &mut elems,
        );
    }
    elems
}

fn process_node_for_body(
    node: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    module_name: &str,
    method_index: &HashMap<String, Vec<FuncKey>>,
    elems: &mut Vec<BodyElement>,
) {
    match node.kind() {
        "for_statement" => {
            let mut loop_body = Vec::new();
            if let Some(b) = node.child_by_field_name("body") {
                let mut c2 = b.walk();
                for ch in b.children(&mut c2) {
                    process_node_for_body(
                        ch,
                        source,
                        current_pkg,
                        imports,
                        module_name,
                        method_index,
                        &mut loop_body,
                    );
                }
            }
            elems.push(BodyElement::Loop { body: loop_body });
        }
        "go_statement" => {
            if let Some(call) = find_call_expression(node) {
                if let Some(fnode) = call.child_by_field_name("function") {
                    let cals = resolve_callee_node(
                        fnode,
                        source,
                        current_pkg,
                        imports,
                        module_name,
                        method_index,
                    );
                    for cal in cals {
                        elems.push(BodyElement::Call { info: CallInfo {
                            callee: cal,
                            goroutine: true,
                        }});
                    }
                }
            }
        }
        "call_expression" => {
            if let Some(fnode) = node.child_by_field_name("function") {
                let cals = resolve_callee_node(
                    fnode,
                    source,
                    current_pkg,
                    imports,
                    module_name,
                    method_index,
                );
                for cal in cals {
                    elems.push(BodyElement::Call { info: CallInfo {
                        callee: cal,
                        goroutine: false,
                    }});
                }
            }
        }
        _ => {
            // Recurse into other nodes (ifs, blocks, expression statements, etc.)
            // to surface calls and inner loops in source order.
            let mut c = node.walk();
            for ch in node.children(&mut c) {
                process_node_for_body(
                    ch,
                    source,
                    current_pkg,
                    imports,
                    module_name,
                    method_index,
                    elems,
                );
            }
        }
    }
}

fn find_call_expression(node: Node) -> Option<Node> {
    if node.kind() == "call_expression" {
        return Some(node);
    }
    let mut c = node.walk();
    for ch in node.children(&mut c) {
        if let Some(f) = find_call_expression(ch) {
            return Some(f);
        }
    }
    None
}

fn populate_body_elements(
    node: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    module_name: &str,
    method_index: &HashMap<String, Vec<FuncKey>>,
    graph: &mut CallGraph,
) {
    match node.kind() {
        "function_declaration" | "method_declaration" => {
            // Build the key similar to extract_function_infos
            if let Some(name_node) = node.child_by_field_name("name") {
                let name = name_node.utf8_text(source.as_bytes()).unwrap_or("??").to_string();
                let recv = if node.kind() == "method_declaration" {
                    extract_receiver_type(node, source).map(|r| Arc::from(r.as_str()))
                } else {
                    None
                };
                let pkg_arc: Arc<str> = Arc::from(current_pkg);
                let key = FuncKey {
                    pkg: pkg_arc,
                    recv,
                    name: Arc::from(name.as_str()),
                };

                if let Some(body) = node.child_by_field_name("body") {
                    let elems = extract_body_elements(
                        body,
                        source,
                        current_pkg,
                        imports,
                        module_name,
                        method_index,
                    );
                    // Overwrite the (empty) entry from pass 1 with the structured body
                    graph.insert(key, elems);
                }
            }
        }
        _ => {}
    }

    // Always recurse to find nested declarations (though Go doesn't nest named funcs usually)
    let mut c = node.walk();
    for ch in node.children(&mut c) {
        populate_body_elements(
            ch,
            source,
            current_pkg,
            imports,
            module_name,
            method_index,
            graph,
        );
    }
}

/// Given a file + line, parse just that file and resolve the callee at the call site.
fn resolve_call_at_line(
    target_file: &Path,
    line: usize,
    module_root: &Path,
    module_name: &str,
    defs: &DefMap,
) -> Option<Callee> {
    let source = fs::read_to_string(target_file).ok()?;
    let mut parser = TsParser::new();
    parser.set_language(&tree_sitter_go::language()).ok()?;
    let tree = parser.parse(source.as_bytes(), None)?;

    let pkg = compute_pkg_path(target_file, module_root, module_name);
    let imports = extract_imports(&tree, &source);

    // Build a lightweight method index from the defs we already have (for start point resolution)
    let mut method_index: HashMap<String, Vec<FuncKey>> = HashMap::new();
    for (k, _) in defs {
        if k.recv.is_some() {
            method_index.entry(k.name.to_string()).or_default().push(k.clone());
        }
    }

    // Find the innermost (actually outermost) call_expression whose range covers the line (1-based)
    let call_node = find_innermost_call_at_line(&tree, line)?;
    let func_node = call_node.child_by_field_name("function")?;

    // Resolve (now returns Vec)
    let candidates = resolve_callee_node(func_node, &source, &pkg, &imports, module_name, &method_index);

    // Prefer a valid local target (for methods this gives us the possible impls)
    let mut first_nonlocal: Option<Callee> = None;
    for cand in candidates {
        if let Callee::Local(ref key) = cand {
            if defs.contains_key(key) {
                return Some(cand);
            }
        } else if first_nonlocal.is_none() {
            first_nonlocal = Some(cand);
        }
    }

    // If we only got descriptive (external or stdlib), still return so user sees it
    first_nonlocal

}

fn find_innermost_call_at_line(tree: &Tree, line: usize) -> Option<Node<'_>> {
    // Collect (start_byte, end_byte) of best match.
    // Prefer *outermost* (largest span) covering the line. This makes nested calls like foo(bar()) pick "foo".
    let mut best_range: Option<(usize, usize)> = None;
    let mut largest = 0usize;

    fn visit(node: Node, target_line: usize, best_range: &mut Option<(usize, usize)>, largest: &mut usize) {
        if node.kind() == "call_expression" {
            let start_l = node.start_position().row + 1;
            let end_l = node.end_position().row + 1;
            if start_l <= target_line && target_line <= end_l {
                let len = node.end_byte().saturating_sub(node.start_byte());
                if len > *largest {
                    *largest = len;
                    *best_range = Some((node.start_byte(), node.end_byte()));
                }
            }
        }
        let mut c = node.walk();
        for child in node.children(&mut c) {
            visit(child, target_line, best_range, largest);
        }
    }

    visit(tree.root_node(), line, &mut best_range, &mut largest);

    let (start_b, _end_b) = best_range?;

    // Second walk to retrieve the actual node for that range (while tree lives)
    fn find_node_at_range(node: Node<'_>, start: usize) -> Option<Node<'_>> {
        if node.start_byte() == start && node.kind() == "call_expression" {
            return Some(node);
        }
        let mut c = node.walk();
        for child in node.children(&mut c) {
            if let Some(found) = find_node_at_range(child, start) {
                return Some(found);
            }
        }
        None
    }

    find_node_at_range(tree.root_node(), start_b)
}

/// Fallback: given a line, find if it's inside one of the indexed functions and return its key.
fn find_enclosing_function_key(
    file: &Path,
    line: usize,
    module_root: &Path,
    module_name: &str,
    _defs: &DefMap,
) -> Option<FuncKey> {
    // Re-parse only this file (very cheap).
    let source = fs::read_to_string(file).ok()?;
    let mut parser = TsParser::new();
    parser.set_language(&tree_sitter_go::language()).ok()?;
    let tree = parser.parse(source.as_bytes(), None)?;

    let pkg = compute_pkg_path(file, module_root, module_name);
    let infos = extract_function_infos(&tree, &source, &pkg, file);

    // Pick function with the largest declaration line that is still <= target line.
    let mut best: Option<&FuncInfo> = None;
    for info in &infos {
        if info.line <= line {
            if best.map_or(true, |prev| info.line > prev.line) {
                best = Some(info);
            }
        }
    }
    best.map(|i| i.key.clone())
}

// ============================================================================
// Printing the execution tree
// ============================================================================

/// Print a nice tree of reachable local calls.
/// We use a path set to avoid infinite recursion on cycles.
/// `expanded` ensures each function's subtree is printed only once globally (reduces repetition)
/// `loop_counter` assigns unique numbers to printed loops.
fn print_execution_tree(
    key: &FuncKey,
    graph: &CallGraph,
    defs: &DefMap,
    depth: usize,
    path: &mut HashSet<FuncKey>,
    expanded: &mut HashSet<FuncKey>,
    loop_counter: &mut usize,
    max_depth: usize,
) {
    if depth > max_depth {
        println!("{}└── {}", "  ".repeat(depth), color_loc("... (max depth reached)"));
        return;
    }

    if !path.insert(key.clone()) {
        let indent = "  ".repeat(depth);
        let colored = if key.recv.is_some() { color_method(&key.display()) } else { color_func(&key.display()) };
        println!("{}{} {}", indent, colored, color_loop("(recursive)"));
        return;
    }

    let indent = "  ".repeat(depth);
    let loc = defs.get(key)
        .map(|(f, l)| {
            color_loc(&format!(" ({}:{})", f.file_name().unwrap_or_default().to_string_lossy(), l))
        })
        .unwrap_or_default();

    // This function is only called when we decided it is the first time to show its details.
    let colored_key = if key.recv.is_some() {
        color_method(&key.display())
    } else {
        color_func(&key.display())
    };
    println!("{}{}{}", indent, colored_key, loc);

    if let Some(body_elements) = graph.get(key) {
        print_body_elements(
            body_elements,
            graph,
            defs,
            depth,
            path,
            expanded,
            loop_counter,
            max_depth,
        );
    }

    path.remove(key);
}

/// Print the structured body elements for a function.
/// Loops are shown with explicit start/end, and calls (incl. goroutine launches) are placed inside.
fn has_printable_content(elements: &[BodyElement]) -> bool {
    for elem in elements {
        match elem {
            BodyElement::Call { .. } => return true,
            BodyElement::Loop { body } => {
                if has_printable_content(body) {
                    return true;
                }
            }
        }
    }
    false
}

fn get_loop_style(id: usize) -> &'static str {
    // Cycle through distinct colors for nested loops. Bold where possible for visibility.
    const STYLES: &[&str] = &["1;36", "1;35", "1;32", "1;33", "1;34", "1;31", "36", "35", "32"];
    STYLES[id % STYLES.len()]
}

fn print_body_elements(
    elements: &[BodyElement],
    graph: &CallGraph,
    defs: &DefMap,
    depth: usize,
    path: &mut HashSet<FuncKey>,
    expanded: &mut HashSet<FuncKey>,
    loop_counter: &mut usize,
    max_depth: usize,
) {
    // Filter to only loops that contain printable content (calls) and all calls.
    let printable: Vec<&BodyElement> = elements
        .iter()
        .filter(|e| match e {
            BodyElement::Call { .. } => true,
            BodyElement::Loop { body } => has_printable_content(body),
        })
        .collect();

    let n = printable.len();
    for (i, elem) in printable.iter().enumerate() {
        let is_last = i == n - 1;
        let prefix = if is_last { "└── " } else { "├── " };
        let ind = "  ".repeat(depth + 1);

        let elem = *elem;
        match elem {
            BodyElement::Call { info } => {
                match &info.callee {
                    Callee::Local(child_key) => {
                        let child_loc = defs.get(child_key)
                            .map(|(f, l)| color_loc(&format!(" ({}:{})", f.file_name().unwrap_or_default().to_string_lossy(), l)))
                            .unwrap_or_default();

                        let base_name = child_key.display();
                        let colored_name = if info.goroutine {
                            color_goroutine(&base_name)
                        } else if child_key.recv.is_some() {
                            color_method(&base_name)
                        } else {
                            color_func(&base_name)
                        };

                        let display_line = if info.goroutine {
                            format!("go {} {}", colored_name, child_loc)
                        } else {
                            format!("{}{}", colored_name, child_loc)
                        };

                        println!("{}{}{}", ind, prefix, display_line);

                        if expanded.insert(child_key.clone()) {
                            print_execution_tree(child_key, graph, defs, depth + 1, path, expanded, loop_counter, max_depth);
                        }
                    }
                    Callee::External(ext) => {
                        let base = ext.to_string();
                        let display = if info.goroutine {
                            color_goroutine(&format!("go {}", base))
                        } else {
                            color_external(&format!("[external] {}", base))
                        };
                        println!("{}{}{}", ind, prefix, display);
                    }
                    Callee::Stdlib(ext) => {
                        let base = ext.to_string();
                        let display = if info.goroutine {
                            color_goroutine(&format!("go {}", base))
                        } else {
                            color_external(&format!("[stdlib] {}", base))  // reuse color or could make specific
                        };
                        println!("{}{}{}", ind, prefix, display);
                    }
                }
            }
            BodyElement::Loop { body } => {
                if has_printable_content(body) {
                    let loop_id = *loop_counter;
                    *loop_counter += 1;
                    let display_num = loop_id + 1;
                    let style = get_loop_style(loop_id);
                    println!("{}{}{} #{}", ind, prefix, paint("loop start", style), display_num);
                    print_body_elements(body, graph, defs, depth + 1, path, expanded, loop_counter, max_depth);
                    // Align "loop end" text to start at the same horizontal position as "loop start"
                    // Use char count (not byte len) because the tree prefixes contain multi-byte unicode box chars.
                    let end_indent = format!("{}{}", ind, " ".repeat(prefix.chars().count()));
                    println!("{}{}{} #{}", end_indent, "", paint("loop end", style), display_num);
                }
            }
        }
    }
}
