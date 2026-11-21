// Package prim declares the data of grammar code at the Exec stage of Code IR (design record 074,
// Printers Take Exec-Stage Units): the primitives that the Flow operations become (design record
// 073), the Go types of their immediates, the registers, the classification of the handle types,
// the labels of unnamed loops and the attributes of a function.
//
// execlower converts each call of a Flow operation into a call of its primitive, which has the
// operation's signature, contract, cost and immediate, so the Flow dialect declares its operations
// with the immediate types of this package. The package imports only the Code IR and Core IR
// packages, and registers the check of the Exec stage: a function at the Exec stage has the
// attributes of this package or none.
package prim
