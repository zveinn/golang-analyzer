I want you to build a tool that analyses a function/method call. it will be a cli that accept a file+line, then analyzess the function on that line and traces all possible execution paths for that functions. It should also trace parameter allocations.

The target programming language is golang.

EXAMPLE CLI USAGE:
$ ./code-analyzer [file] [line]

NOTES:
 - this project will be doing code analysis, so simple string matching will not be enough in many cases. I need you to research ways to tokenize/analyse code before you start.
 - feel free to make code examples inside ./examples for you to test the analyzer. Make sure to test complex things like generics, channels, goroutines, interface matching, atomics, loops, etc..

RULES:
 - do not trace function calls into stdlib packages or modules. Keep the tracing isolated to the current project.
 - label function calls with the following labels: stdlib, module, local
   - stdlib = golang internal stdlib methods/functions
   - module = imported module methods/functions
   - local = methods/functions that are native to the current code base
 - Only trace variables that are used as parameters in function calls throughout the trace or return variables from function calls
 - when we detect a golang channel read/write I want you to include both ends of the channel. If it's a write, I want to see readers, if it's a read I want to see writers.
 - show all loops: when a loop appears in the trace, I want you to wrap the functions calles within the loop with a "loop N" label, N being a number that increments by 1 for every loop found
 - highlight goroutine launches

OUTPUT:
The output should be the execution trace with indentation based on call levels.
