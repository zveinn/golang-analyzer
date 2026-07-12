use clap::Parser;
use std::path::{Path, PathBuf};
use std::collections::{HashMap, HashSet};
use std::fs;
use std::sync::Arc;
use rayon::prelude::*;
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
    /// Call through an interface (the "exact interface" the user asked for)
    Interface {
        pkg: String,
        interface: String,
        method: String,
    },
}

#[derive(Clone, Debug, serde::Serialize)]
struct CallInfo {
    callee: Callee,
    goroutine: bool,
    /// Raw source text of each argument expression at the call site (for tracing).
    args: Vec<String>,
}

#[derive(Clone, Debug, serde::Serialize)]
#[serde(tag = "kind")]
enum BodyElement {
    Call { info: CallInfo },
    Loop { body: Vec<BodyElement> },
    /// Variable definition (from var := or var decl). Used for intra-function
    /// parameter/variable tracing. Not displayed as a visible row in UI.
    Def { name: String, rhs: String },
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
    /// Param names for every function in the graph (for cross-frame tracing).
    params: Vec<(FuncKey, Vec<String>)>,
    /// Parameters of the root function (starting point for tracing).
    root_params: Vec<String>,
}

fn perform_analysis(file: &Path, line: usize) -> Result<AnalysisPayload, String> {
    let abs_file = if file.is_absolute() {
        file.to_path_buf()
    } else {
        std::env::current_dir().map_err(|e| e.to_string())?.join(file)
    };

    let (module_root, module_name) = find_go_module(&abs_file)
        .ok_or_else(|| "Could not find go.mod".to_string())?;

    let (graph, defs, interface_index, package_level_vars, param_map) = build_call_graph(&module_root, &module_name)
        .map_err(|e| e.to_string())?;

    let mut start_callee = resolve_call_at_line(&abs_file, line, &module_root, &module_name, &defs, &interface_index, &mut HashMap::new(), &package_level_vars);

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
            let root_params = param_map.get(&key).cloned().unwrap_or_default();
            let params_list: Vec<_> = param_map.into_iter().collect();
            Ok(AnalysisPayload { root: key, body, graph: graph_list, params: params_list, root_params })
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
                params: vec![],
                root_params: vec![],
            })
        }
        Callee::Interface { pkg, interface, method } => {
            let fake_key = FuncKey {
                pkg: Arc::from(format!("{}.{}", pkg, interface).as_str()),
                recv: None,
                name: Arc::from(method.as_str()),
            };
            Ok(AnalysisPayload {
                root: fake_key,
                body: vec![],
                graph: vec![],
                params: vec![],
                root_params: vec![],
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

    let (graph, defs, interface_index, package_level_vars, _param_map) = build_call_graph(&module_root, &module_name)?;

    // Locate the starting callee from the call site
    let mut start_callee = resolve_call_at_line(&abs_file, line, &module_root, &module_name, &defs, &interface_index, &mut HashMap::new(), &package_level_vars);

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
        Callee::Interface { pkg, interface, method } => {
            println!("\n{}", paint("Call at given location is to interface method (not following concrete impls):", "1"));
            println!("  [interface] {}.{}.{}", pkg, interface, method);
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
/// Also collects parameter names per function for tracing.
fn build_call_graph(module_root: &Path, module_name: &str) -> Result<(CallGraph, DefMap, HashMap<String, HashMap<String, HashSet<String>>>, HashMap<String, HashMap<String, String>>, HashMap<FuncKey, Vec<String>>), Box<dyn std::error::Error>> {
    let go_files = collect_go_files(module_root);

    let mut parser = TsParser::new();
    parser.set_language(&tree_sitter_go::language())
        .expect("failed to load tree-sitter-go grammar");

    let mut graph: CallGraph = HashMap::new();
    let mut defs: DefMap = HashMap::new();
    let mut method_index: HashMap<String, Vec<FuncKey>> = HashMap::new();
    // pkg -> interface_name -> set of method names it declares
    let mut interface_index: HashMap<String, HashMap<String, HashSet<String>>> = HashMap::new();
    let mut package_level_vars: HashMap<String, HashMap<String, String>> = HashMap::new(); // pkg -> varname -> type

    // === PASS 1: Collect all definitions and method names in parallel ===
    // Parsing and tree walking are CPU-bound, so we parallelize with rayon.
    // We collect results then merge (merge is fast and avoids locks during parse).
    #[derive(Default)]
    struct FileDefs {
        pkg: String,
        func_infos: Vec<FuncInfo>,
        ifaces: HashMap<String, HashSet<String>>,
        method_defs: Vec<FuncKey>, // only those with recv for method_index
        imports: HashMap<String, String>,
        typed_vars: HashMap<String, String>, // top level var name -> type
    }

    let def_results: Vec<FileDefs> = go_files.par_iter().map(|file_path| {
        let mut res = FileDefs::default();
        let source = match fs::read_to_string(file_path) {
            Ok(s) => s,
            Err(_) => return res,
        };
        if source.trim().is_empty() { return res; }

        let mut p = TsParser::new();
        p.set_language(&tree_sitter_go::language()).expect("parser");
        let tree = match p.parse(source.as_bytes(), None) {
            Some(t) => t,
            None => return res,
        };

        res.pkg = compute_pkg_path(file_path, module_root, module_name);
        res.func_infos = extract_function_infos(&tree, &source, &res.pkg, file_path);
        res.ifaces = extract_interfaces(&tree, &source, &res.pkg);
        res.imports = extract_imports(&tree, &source);

        for info in &res.func_infos {
            if info.key.recv.is_some() {
                res.method_defs.push(info.key.clone());
            }
        }

        // Extract top level typed vars for globals
        res.typed_vars = extract_top_level_typed_vars(&tree, &source, &res.pkg, &res.imports);

        // Note: we don't drop explicitly; they go out of scope.
        res
    }).collect();

    // Merge results sequentially
    for r in def_results {
        for info in r.func_infos {
            defs.entry(info.key.clone()).or_insert((info.file.clone(), info.line));
            graph.entry(info.key.clone()).or_insert_with(Vec::new);
        }
        for (iface, methods) in r.ifaces {
            interface_index
                .entry(r.pkg.clone())
                .or_default()
                .insert(iface, methods);
        }
        for (var, typ) in r.typed_vars {
            package_level_vars
                .entry(r.pkg.clone())
                .or_default()
                .insert(var, typ);
        }
        for k in r.method_defs {
            method_index
                .entry(k.name.to_string())
                .or_default()
                .push(k);
        }
    }

    let mut param_map: HashMap<FuncKey, Vec<String>> = HashMap::new();

    // === PASS 2: Extract calls in parallel ===
    // Each file's body extraction is independent once indexes are built.
    #[derive(Default)]
    struct FileBodies {
        pkg: String,
        bodies: Vec<(FuncKey, Vec<BodyElement>)>,
        params: Vec<(FuncKey, Vec<String>)>,
    }

    let body_results: Vec<FileBodies> = go_files.par_iter().map(|file_path| {
        let mut res = FileBodies::default();
        let source = match fs::read_to_string(file_path) {
            Ok(s) => s,
            Err(_) => return res,
        };
        if source.trim().is_empty() { return res; }

        let mut p = TsParser::new();
        p.set_language(&tree_sitter_go::language()).expect("parser");
        let tree = match p.parse(source.as_bytes(), None) {
            Some(t) => t,
            None => return res,
        };

        res.pkg = compute_pkg_path(file_path, module_root, module_name);
        let imports = extract_imports(&tree, &source);

        // We need a temporary graph for this file's functions only, then collect.
        let mut file_graph: CallGraph = HashMap::new();
        let mut file_params: HashMap<FuncKey, Vec<String>> = HashMap::new();
        let root = tree.root_node();
        // First, make sure entries exist for funcs in this file (we can re-extract or use previous).
        // For simplicity, we call populate which inserts into the provided graph.
        // To avoid global mutation, we use a per-file graph here.
        populate_body_elements(
            root,
            &source,
            &res.pkg,
            &imports,
            module_name,
            &method_index,
            &interface_index,
            &package_level_vars,
            &mut file_graph,
            &mut file_params,
        );

        for (k, b) in file_graph {
            res.bodies.push((k, b));
        }
        res.params = file_params.into_iter().collect();

        res
    }).collect();

    // Merge bodies and params
    for r in body_results {
        for (k, b) in r.bodies {
            graph.insert(k, b);
        }
        for (k, ps) in r.params {
            param_map.insert(k, ps);
        }
    }

    // Post-process:
    // - Convert any Local that has no definition (e.g. func vars like doCMD, or type conversions) into External
    //   so we still surface them (especially important for goroutine annotations).
    for body in graph.values_mut() {
        clean_body_elements(body, &defs);
    }

    Ok((graph, defs, interface_index, package_level_vars, param_map))
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
                let callees = resolve_callee_node(func_node, source, current_pkg, imports, module_name, method_index, &HashMap::new(), &mut HashMap::new(), &HashMap::new());
                if !callees.is_empty() {
                    let infos: Vec<CallInfo> = callees
                        .into_iter()
                        .map(|c| CallInfo {
                            callee: c,
                            goroutine: now_in_goroutine,
                            args: vec![],
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
    interface_index: &HashMap<String, HashMap<String, HashSet<String>>>,
    scope: &HashMap<String, String>,
    package_level_vars: &HashMap<String, HashMap<String, String>>,
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
            // Use scope to find the static type of the operand (receiver).
            let mut out = vec![];

            let receiver_type = scope.get(&operand_text).cloned().or_else(|| {
                package_level_vars.get(current_pkg).and_then(|m| m.get(&operand_text)).cloned()
            });

            if let Some(typ) = &receiver_type {
                let normalized = normalize_type(typ.clone(), imports, current_pkg);

                // 1. Check if it matches a known interface
                if let Some((iface_pkg, iface_name)) = find_interface_for_type(&normalized, interface_index) {
                    if let Some(ifaces) = interface_index.get(&iface_pkg) {
                        if let Some(methods) = ifaces.get(&iface_name) {
                            if methods.contains(&called_name) {
                                return vec![Callee::Interface {
                                    pkg: iface_pkg,
                                    interface: iface_name,
                                    method: called_name,
                                }];
                            }
                        }
                    }
                }

                // 2. Try to find exact concrete method for this receiver type
                if let Some(exact) = find_exact_method(method_index, &normalized, &called_name) {
                    return vec![Callee::Local(exact)];
                }
            }

            // If we reach here without a known receiver type, do not explode into
            // all name-matching methods or every iface sharing the method name.
            // Report as a descriptive external call instead.
            if out.is_empty() {
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
                return resolve_callee_node(inner, source, current_pkg, imports, module_name, method_index, interface_index, scope, package_level_vars);
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

/// Extract raw argument expression texts from a call_expression node.
fn extract_call_args(call_node: Node, source: &str) -> Vec<String> {
    let mut args = Vec::new();
    if let Some(args_node) = call_node.child_by_field_name("arguments") {
        let mut cursor = args_node.walk();
        for child in args_node.children(&mut cursor) {
            match child.kind() {
                "," | "(" | ")" => continue,
                _ => {
                    let text = child.utf8_text(source.as_bytes()).unwrap_or("").trim().to_string();
                    if !text.is_empty() {
                        args.push(text);
                    }
                }
            }
        }
    }
    args
}

/// Extract formal parameter names (including receiver for methods) from a
/// function_declaration or method_declaration.
fn extract_params(node: Node, source: &str) -> Vec<String> {
    let mut out = Vec::new();

    // Receiver (for methods) — treated as a parameter for tracing (the "self").
    if node.kind() == "method_declaration" {
        if let Some(recv_list) = node.child_by_field_name("receiver") {
            for i in 0..recv_list.child_count() {
                if let Some(param_decl) = recv_list.child(i) {
                    if param_decl.kind() == "parameter_declaration" {
                        for j in 0..param_decl.child_count() {
                            if let Some(ch) = param_decl.child(j) {
                                if ch.kind() == "identifier" {
                                    let nm = ch.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                                    if !nm.is_empty() {
                                        out.push(nm);
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // Regular parameters
    if let Some(params_list) = node.child_by_field_name("parameters") {
        for i in 0..params_list.child_count() {
            if let Some(param_decl) = params_list.child(i) {
                if param_decl.kind() == "parameter_declaration" {
                    for j in 0..param_decl.child_count() {
                        if let Some(ch) = param_decl.child(j) {
                            if ch.kind() == "identifier" {
                                let nm = ch.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                                if !nm.is_empty() {
                                    out.push(nm);
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    out
}

/// Build initial type scope for a function/method from its signature (receiver + parameters).
fn build_signature_scope(node: Node, source: &str, imports: &HashMap<String, String>, current_pkg: &str) -> HashMap<String, String> {
    let mut scope: HashMap<String, String> = HashMap::new();

    // Receiver for methods
    if node.kind() == "method_declaration" {
        if let Some(recv_list) = node.child_by_field_name("receiver") {
            for i in 0..recv_list.child_count() {
                if let Some(param_decl) = recv_list.child(i) {
                    if param_decl.kind() == "parameter_declaration" {
                        let mut var_name = String::new();
                        let mut typ = String::new();
                        for j in 0..param_decl.child_count() {
                            if let Some(ch) = param_decl.child(j) {
                                match ch.kind() {
                                    "identifier" => var_name = ch.utf8_text(source.as_bytes()).unwrap_or("").to_string(),
                                    "pointer_type" | "type_identifier" | "qualified_type" => {
                                        typ = ch.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                                    }
                                    _ => {}
                                }
                            }
                        }
                        if !var_name.is_empty() && !typ.is_empty() {
                            scope.insert(var_name, normalize_type(typ, imports, current_pkg));
                        }
                    }
                }
            }
        }
    }

    // Parameters
    if let Some(params_list) = node.child_by_field_name("parameters") {
        for i in 0..params_list.child_count() {
            if let Some(param_decl) = params_list.child(i) {
                if param_decl.kind() == "parameter_declaration" {
                    let mut var_names = vec![];
                    let mut typ = String::new();
                    for j in 0..param_decl.child_count() {
                        if let Some(ch) = param_decl.child(j) {
                            match ch.kind() {
                                "identifier" => var_names.push(ch.utf8_text(source.as_bytes()).unwrap_or("").to_string()),
                                "pointer_type" | "type_identifier" | "qualified_type" | "array_type" | "map_type" | "chan_type" => {
                                    typ = ch.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                                }
                                _ => {}
                            }
                        }
                    }
                    let norm_typ = if !typ.is_empty() {
                        normalize_type(typ, imports, current_pkg)
                    } else {
                        String::new()
                    };
                    for vname in var_names {
                        if !vname.is_empty() && !norm_typ.is_empty() {
                            scope.insert(vname, norm_typ.clone());
                        }
                    }
                }
            }
        }
    }

    scope
}

fn normalize_type(typ: String, imports: &HashMap<String, String>, current_pkg: &str) -> String {
    let t = typ.trim().trim_start_matches('*').to_string();
    if t.contains('.') {
        return t;
    }
    if let Some(full) = imports.get(&t) {
        return full.clone();
    }
    if t == "error" || t == "string" || t == "int" || t == "bool" { // builtins
        return t;
    }
    format!("{}.{}", current_pkg, t)
}

/// Best-effort inference of a raw receiver type string (e.g. "*Foo", "Bar", "pkg.Qux")
/// from a RHS expression in a short declaration or var initializer.
fn infer_raw_type_from_rhs(expr: Node, source: &str) -> Option<String> {
    let txt = expr.utf8_text(source.as_bytes()).unwrap_or("").trim().to_string();
    if txt.is_empty() {
        return None;
    }
    match expr.kind() {
        "composite_literal" => {
            // type is usually the first child (type_identifier, qualified_type, etc.)
            if let Some(first) = expr.child(0) {
                let k = first.kind();
                if k == "type_identifier" || k == "qualified_type" || k.ends_with("type") || k == "pointer_type" || k == "identifier" {
                    let t = first.utf8_text(source.as_bytes()).unwrap_or("").trim().to_string();
                    if !t.is_empty() {
                        return Some(t);
                    }
                }
            }
            // fallback: before '{' 
            if let Some(pos) = txt.find('{') {
                let t = txt[..pos].trim().to_string();
                if !t.is_empty() {
                    return Some(t);
                }
            }
            None
        }
        "unary_expression" => {
            if txt.starts_with('&') {
                let rest = txt[1..].trim_start().to_string();
                if let Some(pos) = rest.find('{') {
                    let base = rest[..pos].trim().to_string();
                    if !base.is_empty() {
                        return Some(if base.starts_with('*') { base } else { format!("*{}", base) });
                    }
                } else if !rest.is_empty() {
                    // &TypeName or &var but we take if looks like exported ident
                    let base = rest.trim().to_string();
                    if base.chars().next().map_or(false, |c| c.is_ascii_uppercase()) || base.starts_with('*') {
                        return Some(if base.starts_with('*') { base } else { format!("*{}", base) });
                    }
                }
            }
            None
        }
        "call_expression" => {
            if let Some(fnode) = expr.child_by_field_name("function") {
                let fname = fnode.utf8_text(source.as_bytes()).unwrap_or("");
                // NewFoo() or newFoo() -> *Foo
                if let Some(stripped) = fname.strip_prefix("New").or_else(|| fname.strip_prefix("new")) {
                    if !stripped.is_empty() && stripped.chars().next().map_or(false, |c| c.is_ascii_uppercase() || stripped.chars().next().map_or(false, |c| c.is_ascii_lowercase())) {
                        return Some(format!("*{}", stripped));
                    }
                }
            }
            None
        }
        _ => None,
    }
}

fn collect_var_types_from_short_decl(
    node: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    scope: &mut HashMap<String, String>,
) {
    // short_var_declaration contains two expression_list: lhs then rhs
    let mut expr_lists: Vec<Node> = Vec::new();
    let mut c = node.walk();
    for ch in node.children(&mut c) {
        if ch.kind() == "expression_list" {
            expr_lists.push(ch);
        }
    }
    if expr_lists.len() < 2 {
        return;
    }
    let lhs = expr_lists[0];
    let rhs = expr_lists[1];

    let mut lhs_names: Vec<String> = Vec::new();
    let mut lc = lhs.walk();
    for ch in lhs.children(&mut lc) {
        if ch.kind() == "identifier" {
            lhs_names.push(ch.utf8_text(source.as_bytes()).unwrap_or("").to_string());
        }
    }

    let mut rhs_exprs: Vec<Node> = Vec::new();
    let mut rc = rhs.walk();
    for ch in rhs.children(&mut rc) {
        rhs_exprs.push(ch);
    }

    for (i, name) in lhs_names.into_iter().enumerate() {
        if name.is_empty() {
            continue;
        }
        let typ_raw = rhs_exprs.get(i).and_then(|re| infer_raw_type_from_rhs(*re, source));
        if let Some(t) = typ_raw {
            let norm = normalize_type(t, imports, current_pkg);
            if !norm.is_empty() {
                scope.insert(name, norm);
            }
        }
    }
}

fn update_scope_from_var_spec(
    spec: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    scope: &mut HashMap<String, String>,
) {
    let mut names = vec![];
    let mut typ = String::new();
    let mut ccc = spec.walk();
    for cch in spec.children(&mut ccc) {
        match cch.kind() {
            "identifier" => names.push(cch.utf8_text(source.as_bytes()).unwrap_or("").to_string()),
            "pointer_type" | "type_identifier" | "qualified_type" => {
                typ = cch.utf8_text(source.as_bytes()).unwrap_or("").to_string();
            }
            _ => {}
        }
    }
    if typ.is_empty() {
        // try infer from value expr if present (var x = NewY() etc)
        let mut vc = spec.walk();
        for cch in spec.children(&mut vc) {
            if matches!(cch.kind(), "expression_list" | "composite_literal" | "call_expression" | "unary_expression" | "identifier") {
                if let Some(inf) = infer_raw_type_from_rhs(cch, source) {
                    typ = inf;
                    break;
                }
            }
        }
    }
    for n in names {
        if !n.is_empty() && !typ.is_empty() {
            scope.insert(n, normalize_type(typ.clone(), imports, current_pkg));
        }
    }
}

fn extract_top_level_typed_vars(tree: &Tree, source: &str, pkg: &str, imports: &HashMap<String, String>) -> HashMap<String, String> {
    let mut vars: HashMap<String, String> = HashMap::new();
    let root = tree.root_node();

    fn visit(node: Node, source: &str, pkg: &str, imports: &HashMap<String, String>, vars: &mut HashMap<String, String>) {
        if node.kind() == "var_declaration" {
            let mut cc = node.walk();
            for ch in node.children(&mut cc) {
                if ch.kind() == "var_spec" {
                    update_scope_from_var_spec(ch, source, pkg, imports, vars);
                }
            }
        }
        let mut c = node.walk();
        for ch in node.children(&mut c) {
            visit(ch, source, pkg, imports, vars);
        }
    }

    visit(root, source, pkg, imports, &mut vars);
    vars
}

/// Try to find if a type corresponds to a known interface.
fn find_interface_for_type(typ: &str, interface_index: &HashMap<String, HashMap<String, HashSet<String>>>) -> Option<(String, String)> {
    let clean = typ.trim_start_matches('*');
    if let Some(dot_pos) = clean.rfind('.') {
        let pkg_part = &clean[..dot_pos];
        let name_part = &clean[dot_pos+1..];
        if let Some(ifaces) = interface_index.get(pkg_part) {
            if ifaces.contains_key(name_part) {
                return Some((pkg_part.to_string(), name_part.to_string()));
            }
        }
    } else {
        // unqualified, search all
        for (p, ifaces) in interface_index {
            if ifaces.contains_key(clean) {
                return Some((p.clone(), clean.to_string()));
            }
        }
    }
    None
}

/// Try to find exact method for a concrete receiver type.
fn find_exact_method(method_index: &HashMap<String, Vec<FuncKey>>, recv_type: &str, method: &str) -> Option<FuncKey> {
    if let Some(cands) = method_index.get(method) {
        let clean_recv = recv_type.trim_start_matches('*');
        for k in cands {
            if let Some(r) = &k.recv {
                let clean_k = r.trim_start_matches('*');
                if clean_k == clean_recv || clean_recv.ends_with(clean_k) || clean_k.ends_with(clean_recv) {
                    return Some(k.clone());
                }
            }
        }
    }
    None
}

/// Extracts interface definitions from a parsed file.
/// Returns map: interface_name -> set of method names declared in it.
fn extract_interfaces(tree: &Tree, source: &str, _pkg: &str) -> HashMap<String, HashSet<String>> {
    let mut interfaces: HashMap<String, HashSet<String>> = HashMap::new();
    let root = tree.root_node();

    fn visit(node: Node, source: &str, interfaces: &mut HashMap<String, HashSet<String>>) {
        if node.kind() == "interface_type" {
            // Find the parent type_spec to get the interface name
            let mut current = node;
            let mut iface_name: Option<String> = None;
            while let Some(parent) = current.parent() {
                if parent.kind() == "type_spec" {
                    if let Some(name_node) = parent.child_by_field_name("name") {
                        iface_name = Some(name_node.utf8_text(source.as_bytes()).unwrap_or("").to_string());
                    }
                    break;
                }
                current = parent;
            }

            if let Some(name) = iface_name {
                let mut methods = HashSet::new();
                // Collect method_spec names
                let mut cursor = node.walk();
                for child in node.children(&mut cursor) {
                    if child.kind() == "method_spec" {
                        if let Some(method_name_node) = child.child_by_field_name("name") {
                            let mname = method_name_node.utf8_text(source.as_bytes()).unwrap_or("").to_string();
                            if !mname.is_empty() {
                                methods.insert(mname);
                            }
                        }
                    }
                }
                if !methods.is_empty() {
                    interfaces.entry(name).or_default().extend(methods);
                }
            }
        }

        let mut c = node.walk();
        for child in node.children(&mut c) {
            visit(child, source, interfaces);
        }
    }

    visit(root, source, &mut interfaces);
    interfaces
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
        (Callee::Interface { pkg: pa, interface: ia, method: ma }, Callee::Interface { pkg: pb, interface: ib, method: mb }) => {
            pa == pb && ia == ib && ma == mb
        }
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
            BodyElement::Def { .. } => {}
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
    interface_index: &HashMap<String, HashMap<String, HashSet<String>>>,
    scope: &mut HashMap<String, String>,
    package_level_vars: &HashMap<String, HashMap<String, String>>,
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
            interface_index,
            scope,
            package_level_vars,
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
    interface_index: &HashMap<String, HashMap<String, HashSet<String>>>,
    scope: &mut HashMap<String, String>,
    package_level_vars: &HashMap<String, HashMap<String, String>>,
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
                        interface_index,
                        scope,
                        package_level_vars,
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
                        interface_index,
                        scope,
                        package_level_vars,
                    );
                    let args = extract_call_args(call, source);
                    for cal in cals {
                        elems.push(BodyElement::Call { info: CallInfo {
                            callee: cal,
                            goroutine: true,
                            args: args.clone(),
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
                    interface_index,
                    scope,
                    package_level_vars,
                );
                let args = extract_call_args(node, source);
                for cal in cals {
                    elems.push(BodyElement::Call { info: CallInfo {
                        callee: cal,
                        goroutine: false,
                        args: args.clone(),
                    }});
                }
            }
        }
        "var_declaration" => {
            let mut cc = node.walk();
            for ch in node.children(&mut cc) {
                if ch.kind() == "var_spec" {
                    update_scope_from_var_spec(ch, source, current_pkg, imports, scope);
                    // Also emit Def (rhs may be empty if only type given)
                    // Try to find a rhs expression for tracing
                    let mut names: Vec<String> = vec![];
                    let mut rhs_text = String::new();
                    let mut inner = ch.walk();
                    for cch in ch.children(&mut inner) {
                        if cch.kind() == "identifier" {
                            names.push(cch.utf8_text(source.as_bytes()).unwrap_or("").to_string());
                        }
                    }
                    // look for possible value expr after =
                    let mut vc = ch.walk();
                    for cch in ch.children(&mut vc) {
                        if matches!(cch.kind(), "composite_literal" | "call_expression" | "unary_expression" | "identifier" | "selector_expression") {
                            let t = cch.utf8_text(source.as_bytes()).unwrap_or("").trim().to_string();
                            if !t.is_empty() && !names.contains(&t) {
                                rhs_text = t;
                                break;
                            }
                        }
                    }
                    for nm in names {
                        if !nm.is_empty() {
                            elems.push(BodyElement::Def { name: nm, rhs: rhs_text.clone() });
                        }
                    }
                }
            }
            // Recurse to find calls inside the declaration expressions
            let mut c = node.walk();
            for ch in node.children(&mut c) {
                process_node_for_body(
                    ch,
                    source,
                    current_pkg,
                    imports,
                    module_name,
                    method_index,
                    interface_index,
                    scope,
                    package_level_vars,
                    elems,
                );
            }
        }
        "short_var_declaration" => {
            collect_var_types_from_short_decl(node, source, current_pkg, imports, scope);

            // Emit Def for tracing (variables created and potentially passed down)
            let mut lhs_names: Vec<String> = Vec::new();
            let mut rhs_exprs: Vec<Node> = Vec::new();
            let mut cc = node.walk();
            let mut saw_lists = 0usize;
            for ch in node.children(&mut cc) {
                if ch.kind() == "expression_list" {
                    if saw_lists == 0 {
                        let mut lc = ch.walk();
                        for idch in ch.children(&mut lc) {
                            if idch.kind() == "identifier" {
                                lhs_names.push(idch.utf8_text(source.as_bytes()).unwrap_or("").to_string());
                            }
                        }
                    } else if saw_lists == 1 {
                        let mut rc = ch.walk();
                        for exch in ch.children(&mut rc) {
                            rhs_exprs.push(exch);
                        }
                    }
                    saw_lists += 1;
                }
            }
            for (i, nm) in lhs_names.into_iter().enumerate() {
                if !nm.is_empty() {
                    let rhs = rhs_exprs.get(i)
                        .map(|e| e.utf8_text(source.as_bytes()).unwrap_or("").trim().to_string())
                        .unwrap_or_default();
                    elems.push(BodyElement::Def { name: nm, rhs });
                }
            }

            // Recurse for any calls inside the RHS expressions
            let mut c = node.walk();
            for ch in node.children(&mut c) {
                process_node_for_body(
                    ch,
                    source,
                    current_pkg,
                    imports,
                    module_name,
                    method_index,
                    interface_index,
                    scope,
                    package_level_vars,
                    elems,
                );
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
                    interface_index,
                    scope,
                    package_level_vars,
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
    interface_index: &HashMap<String, HashMap<String, HashSet<String>>>,
    package_level_vars: &HashMap<String, HashMap<String, String>>,
    graph: &mut CallGraph,
    params: &mut HashMap<FuncKey, Vec<String>>,
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
                    let mut func_scope = build_signature_scope(node, source, imports, current_pkg);
                    let elems = extract_body_elements(
                        body,
                        source,
                        current_pkg,
                        imports,
                        module_name,
                        method_index,
                        interface_index,
                        &mut func_scope,
                        package_level_vars,
                    );
                    // Overwrite the (empty) entry from pass 1 with the structured body
                    graph.insert(key.clone(), elems);
                    let pnames = extract_params(node, source);
                    params.insert(key, pnames);
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
            interface_index,
            package_level_vars,
            graph,
            params,
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
    interface_index: &HashMap<String, HashMap<String, HashSet<String>>>,
    _scope: &mut HashMap<String, String>,
    package_level_vars: &HashMap<String, HashMap<String, String>>,
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

    // Build a scope for the call site by using the enclosing function's signature
    // plus any var/short-var declarations that appear before this call in source order.
    let mut site_scope: HashMap<String, String> = HashMap::new();
    if let Some(enclosing) = find_enclosing_func_node(&tree, call_node.start_byte()) {
        site_scope = build_signature_scope(enclosing, &source, &imports, &pkg);
        if let Some(body) = enclosing.child_by_field_name("body") {
            collect_decls_before(body, &source, &pkg, &imports, call_node.start_byte(), &mut site_scope);
        }
    }

    // Resolve (now returns Vec). site_scope has signature + preceding locals; resolve_callee_node also falls back to package_level_vars.
    let candidates = resolve_callee_node(func_node, &source, &pkg, &imports, module_name, &method_index, interface_index, &site_scope, package_level_vars);

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

fn find_enclosing_func_node<'a>(tree: &'a Tree, byte_pos: usize) -> Option<Node<'a>> {
    fn visit<'a>(node: Node<'a>, pos: usize) -> Option<Node<'a>> {
        if node.kind() == "function_declaration" || node.kind() == "method_declaration" {
            if let Some(body) = node.child_by_field_name("body") {
                if pos >= body.start_byte() && pos < body.end_byte() {
                    return Some(node);
                }
            }
        }
        let mut c = node.walk();
        for ch in node.children(&mut c) {
            if let Some(f) = visit(ch, pos) {
                return Some(f);
            }
        }
        None
    }
    visit(tree.root_node(), byte_pos)
}

fn collect_decls_before(
    node: Node,
    source: &str,
    current_pkg: &str,
    imports: &HashMap<String, String>,
    cutoff: usize,
    scope: &mut HashMap<String, String>,
) {
    if node.start_byte() >= cutoff {
        return;
    }
    match node.kind() {
        "var_declaration" => {
            let mut cc = node.walk();
            for ch in node.children(&mut cc) {
                if ch.kind() == "var_spec" {
                    update_scope_from_var_spec(ch, source, current_pkg, imports, scope);
                }
            }
        }
        "short_var_declaration" => {
            collect_var_types_from_short_decl(node, source, current_pkg, imports, scope);
        }
        _ => {}
    }
    let mut c = node.walk();
    for ch in node.children(&mut c) {
        collect_decls_before(ch, source, current_pkg, imports, cutoff, scope);
    }
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
            BodyElement::Def { .. } => {} // internal for tracing, not printed
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
    // Defs are for tracing only and are filtered out of the visible tree.
    let printable: Vec<&BodyElement> = elements
        .iter()
        .filter(|e| match e {
            BodyElement::Call { .. } => true,
            BodyElement::Loop { body } => has_printable_content(body),
            BodyElement::Def { .. } => false,
        })
        .collect();

    let n = printable.len();
    for (i, elem) in printable.iter().enumerate() {
        let is_last = i == n - 1;
        let prefix = if is_last { "└── " } else { "├── " };
        let ind = "  ".repeat(depth + 1);

        let elem = *elem;
        match elem {
            BodyElement::Def { .. } => {} // should have been filtered
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
                    Callee::Interface { pkg, interface, method } => {
                        let desc = format!("{}.{}.{}", pkg, interface, method);
                        let display = if info.goroutine {
                            color_goroutine(&format!("go {}", desc))
                        } else {
                            // Special color or reuse stdlib-like for interfaces
                            color_external(&format!("[interface] {}", desc))
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
