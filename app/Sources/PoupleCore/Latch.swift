import Foundation

/// A value that is set once and awaited by any number of tasks. Waiting is cancellation-aware: a cancelled
/// waiter gets `nil` instead of blocking, which lets a timeout race against it.
final class Latch<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Value?
    private var waiters: [Int: CheckedContinuation<Value?, Never>] = [:]
    private var nextID = 0

    /// Sets the value; later calls are ignored.
    func set(_ newValue: Value) {
        lock.lock()
        guard value == nil else { lock.unlock(); return }
        value = newValue
        let pending = waiters
        waiters = [:]
        lock.unlock()
        for continuation in pending.values { continuation.resume(returning: newValue) }
    }

    var current: Value? {
        lock.lock()
        defer { lock.unlock() }
        return value
    }

    func wait() async -> Value? {
        let id = lock.withLock { () -> Int in
            nextID += 1
            return nextID
        }
        return await withTaskCancellationHandler {
            await withCheckedContinuation { (continuation: CheckedContinuation<Value?, Never>) in
                lock.lock()
                if let value {
                    lock.unlock()
                    continuation.resume(returning: value)
                } else if Task.isCancelled {
                    lock.unlock()
                    continuation.resume(returning: nil)
                } else {
                    waiters[id] = continuation
                    lock.unlock()
                }
            }
        } onCancel: {
            lock.lock()
            let continuation = waiters.removeValue(forKey: id)
            lock.unlock()
            continuation?.resume(returning: nil)
        }
    }
}

/// Runs `operation` and gives up after `duration`: the result, or nil on timeout. `operation` must honour cancellation.
func withTimeout<T: Sendable>(_ duration: Duration, _ operation: @escaping @Sendable () async -> T?) async -> T? {
    await withTaskGroup(of: T?.self) { group in
        group.addTask { await operation() }
        group.addTask {
            try? await Task.sleep(for: duration)
            return nil
        }
        let first = await group.next() ?? nil
        group.cancelAll()
        return first
    }
}
