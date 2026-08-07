use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::process;

use zero_rule::protocol::decode_json;
use zero_rule::zrs::{encode, verify, VerifyMode};
use zero_rule::RuleSetCompiler;

const VERSION: &str = env!("CARGO_PKG_VERSION");

type Result<T> = std::result::Result<T, Box<dyn Error>>;

fn main() {
    if let Err(error) = run() {
        eprintln!("zrs-compiler: {error}");
        process::exit(1);
    }
}

fn run() -> Result<()> {
    let mut arguments = std::env::args().skip(1);
    let Some(command) = arguments.next() else {
        return Err(usage().into());
    };

    match command.as_str() {
        "--version" | "version" => {
            println!("zrs-compiler {VERSION}");
            Ok(())
        }
        "compile" => {
            let mut input = None;
            let mut output = None;
            while let Some(argument) = arguments.next() {
                match argument.as_str() {
                    "--input" => input = arguments.next().map(PathBuf::from),
                    "--output" => output = arguments.next().map(PathBuf::from),
                    _ => return Err(format!("unknown argument {argument:?}; {}", usage()).into()),
                }
            }
            let input = input.ok_or_else(usage)?;
            let output = output.ok_or_else(usage)?;
            compile(&input, &output)
        }
        _ => Err(format!("unknown command {command:?}; {}", usage()).into()),
    }
}

fn compile(input: &Path, output: &Path) -> Result<()> {
    let source = fs::read(input)?;
    let rules = decode_json(&source)?;
    let (compiled, _) = RuleSetCompiler.compile(rules)?;
    let artifact = encode(&compiled)?;
    verify(&artifact, VerifyMode::FullChecksum)?;

    if let Some(parent) = output.parent() {
        fs::create_dir_all(parent)?;
    }
    let temporary = output.with_extension(format!("tmp-{}", process::id()));
    let result = (|| -> Result<()> {
        fs::write(&temporary, &artifact)?;
        fs::rename(&temporary, output)?;
        Ok(())
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}

fn usage() -> String {
    "usage: zrs-compiler compile --input <source.json> --output <rules.zrs>".to_owned()
}
