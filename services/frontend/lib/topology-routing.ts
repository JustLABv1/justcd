export type RoutingPoint = { x: number; y: number }
export type RoutingBox = RoutingPoint & { id: string; width: number; height: number; compact: boolean }
export type RoutedConnection = { sourceHandle: string; targetHandle: string; path: string; points: RoutingPoint[]; labelX: number; labelY: number }
type Port = { side: string; anchor: RoutingPoint; escape: RoutingPoint; axis: number }
const clearance = 12, bendPenalty = 18

function ports(box: RoutingBox): Port[] {
  const { x, y, width, height, compact } = box
  // Core nodes never exit through their labels below the icon. Compact nodes
  // never exit to the right through their inline name/kind/status text.
  return compact ? [
    { side: "left", anchor: { x: x + 8, y: y + 28 }, escape: { x: x - clearance, y: y + 28 }, axis: 0 },
    { side: "top", anchor: { x: x + 26, y: y + 10 }, escape: { x: x + 26, y: y - clearance }, axis: 1 },
    { side: "bottom", anchor: { x: x + 26, y: y + 46 }, escape: { x: x + 26, y: y + height + clearance }, axis: 1 },
  ] : [
    { side: "left", anchor: { x: x + 70, y: y + 38 }, escape: { x: x - clearance, y: y + 38 }, axis: 0 },
    { side: "right", anchor: { x: x + 130, y: y + 38 }, escape: { x: x + width + clearance, y: y + 38 }, axis: 0 },
    { side: "top", anchor: { x: x + 100, y: y + 8 }, escape: { x: x + 100, y: y - clearance }, axis: 1 },
  ]
}

/** Axis-aligned segment intersection, including resource labels and status text. */
export function routeSegmentIntersectsBox(a: RoutingPoint, b: RoutingPoint, box: RoutingPoint & { width: number; height: number }, padding = 0) {
  const left = box.x - padding, right = box.x + box.width + padding, top = box.y - padding, bottom = box.y + box.height + padding
  if (a.y === b.y) return a.y > top && a.y < bottom && Math.max(a.x, b.x) > left && Math.min(a.x, b.x) < right
  if (a.x === b.x) return a.x > left && a.x < right && Math.max(a.y, b.y) > top && Math.min(a.y, b.y) < bottom
  return true
}
const distance = (a: RoutingPoint, b: RoutingPoint) => Math.abs(a.x - b.x) + Math.abs(a.y - b.y)

function roundedPath(points: RoutingPoint[]) {
  let path = `M ${points[0].x},${points[0].y}`
  for (let i = 1; i < points.length - 1; i++) {
    const previous = points[i - 1], point = points[i], next = points[i + 1]
    const radius = Math.min(28, distance(previous, point) / 2, distance(point, next) / 2)
    const before = { x: point.x + Math.sign(previous.x - point.x) * radius, y: point.y + Math.sign(previous.y - point.y) * radius }
    const after = { x: point.x + Math.sign(next.x - point.x) * radius, y: point.y + Math.sign(next.y - point.y) * radius }
    path += ` L ${before.x},${before.y} Q ${point.x},${point.y} ${after.x},${after.y}`
  }
  return `${path} L ${points.at(-1)!.x},${points.at(-1)!.y}`
}

// Prefer a free-flowing cubic. Sampling at subpixel spacing checks the actual
// curve, including its endpoint tangents, against complete resource rectangles.
function floatingCurve(boxes: RoutingBox[], source: RoutingBox, target: RoutingBox, starts: Port[], goals: Port[]): RoutedConnection | null {
  let best: (RoutedConnection & { length: number }) | null = null
  for (const start of starts) for (const goal of goals) {
    const a = start.anchor, b = goal.anchor
    const reach = Math.min(160, Math.max(40, Math.hypot(b.x - a.x, b.y - a.y) * .4))
    const tangent = (port: Port) => ({ x: port.anchor.x + Math.sign(port.escape.x - port.anchor.x) * reach, y: port.anchor.y + Math.sign(port.escape.y - port.anchor.y) * reach })
    const c = tangent(start), d = tangent(goal)
    const at = (t: number) => { const u = 1 - t; return { x: u*u*u*a.x + 3*u*u*t*c.x + 3*u*t*t*d.x + t*t*t*b.x, y: u*u*u*a.y + 3*u*u*t*c.y + 3*u*t*t*d.y + t*t*t*b.y } }
    const steps = Math.ceil((Math.hypot(c.x-a.x,c.y-a.y)+Math.hypot(d.x-c.x,d.y-c.y)+Math.hypot(b.x-d.x,b.y-d.y))*2)
    let safe = true, length = 0, previous = a
    for (let i = 1; i < steps; i++) {
      const p = at(i / steps)
      if (boxes.some(box => {
        // Endpoint stubs must leave the icon's enclosing label rectangle.
        const endpointClear = (port: Port, endpoint: RoutingBox) => port.side === "left" ? p.x <= port.anchor.x : port.side === "right" ? p.x >= port.anchor.x && p.y < endpoint.y + 68 : port.side === "top" ? p.y <= port.anchor.y : p.y >= port.anchor.y && (p.x <= endpoint.x + 44 || p.y >= endpoint.y + endpoint.height)
        if (box.id === source.id && endpointClear(start, source)) return false
        if (box.id === target.id && endpointClear(goal, target)) return false
        return p.x > box.x - 5 && p.x < box.x + box.width + 5 && p.y > box.y - 5 && p.y < box.y + box.height + 5
      })) { safe = false; break }
      length += Math.hypot(p.x-previous.x,p.y-previous.y); previous = p
    }
    if (safe && (!best || length < best.length)) {
      const label = at(.5)
      best = { sourceHandle: `source-${start.side}`, targetHandle: `target-${goal.side}`, path: `M ${a.x},${a.y} C ${c.x},${c.y} ${d.x},${d.y} ${b.x},${b.y}`, points: [a, start.escape, goal.escape, b], labelX: label.x, labelY: label.y, length }
    }
  }
  return best
}

type Entry = { state: number; cost: number; estimate: number }
class Queue {
  private values: Entry[] = []
  push(entry: Entry) {
    let index = this.values.length
    this.values.push(entry)
    while (index > 0) {
      const parent = Math.floor((index - 1) / 2)
      if (this.values[parent].estimate <= entry.estimate) break
      this.values[index] = this.values[parent]; index = parent
    }
    this.values[index] = entry
  }
  pop() {
    const first = this.values[0], last = this.values.pop()
    if (!this.values.length || !last) return first
    let index = 0
    while (index * 2 + 1 < this.values.length) {
      let child = index * 2 + 1
      if (child + 1 < this.values.length && this.values[child + 1].estimate < this.values[child].estimate) child++
      if (this.values[child].estimate >= last.estimate) break
      this.values[index] = this.values[child]; index = child
    }
    this.values[index] = last
    return first
  }
}

/** Rectilinear A* over obstacle boundaries; endpoint ports are selected together
 * with the route, rather than choosing a port that forces a line through text. */
export function routeTopologyConnection(boxes: RoutingBox[], sourceId: string, targetId: string): RoutedConnection | null {
  const source = boxes.find(box => box.id === sourceId), target = boxes.find(box => box.id === targetId)
  if (!source || !target || sourceId === targetId) return null
  const inside = (p: RoutingPoint) => boxes.some(box => p.x > box.x - clearance && p.x < box.x + box.width + clearance && p.y > box.y - clearance && p.y < box.y + box.height + clearance)
  const available = (box: RoutingBox) => ports(box).filter(port => !inside(port.escape) && !boxes.some(other => other.id !== box.id && routeSegmentIntersectsBox(port.anchor, port.escape, other, 4)))
  const starts = available(source), goals = available(target)
  if (!starts.length || !goals.length) return null
  const floating = floatingCurve(boxes, source, target, starts, goals)
  if (floating) return floating
  const xs = [...new Set([...boxes.flatMap(box => [box.x - clearance, box.x + box.width + clearance]), ...starts.map(port => port.escape.x), ...goals.map(port => port.escape.x)])].sort((a, b) => a - b)
  const ys = [...new Set([...boxes.flatMap(box => [box.y - clearance, box.y + box.height + clearance]), ...starts.map(port => port.escape.y), ...goals.map(port => port.escape.y)])].sort((a, b) => a - b)
  const index = (p: RoutingPoint) => ys.indexOf(p.y) * xs.length + xs.indexOf(p.x)
  const point = (id: number): RoutingPoint => ({ x: xs[id % xs.length], y: ys[Math.floor(id / xs.length)] })
  const heuristic = (p: RoutingPoint) => Math.min(...goals.map(goal => distance(p, goal.escape)))
  const queue = new Queue(), costs = new Map<number, number>(), previous = new Map<number, number>(), origins = new Map<number, Port>()
  const clearSegments = new Map<string, boolean>()
  for (const start of starts) {
    const state = index(start.escape) * 2 + start.axis, cost = distance(start.anchor, start.escape)
    costs.set(state, cost); origins.set(state, start); queue.push({ state, cost, estimate: cost + heuristic(start.escape) })
  }
  const goalIndices = new Map<number, Port[]>(goals.map(goal => [index(goal.escape), goals.filter(other => index(other.escape) === index(goal.escape))]))
  let best: { state: number; goal: Port; cost: number } | undefined
  for (let entry = queue.pop(); entry; entry = queue.pop()) {
    if (best && entry.estimate >= best.cost) break
    if (entry.cost !== costs.get(entry.state)) continue
    const id = Math.floor(entry.state / 2), axis = entry.state % 2, current = point(id), ix = id % xs.length, iy = Math.floor(id / xs.length)
    for (const goal of goalIndices.get(id) ?? []) {
      const cost = entry.cost + distance(goal.escape, goal.anchor) + (axis === goal.axis ? 0 : bendPenalty)
      if (!best || cost < best.cost) best = { state: entry.state, goal, cost }
    }
    for (const [nx, ny, direction] of [[ix - 1, iy, 0], [ix + 1, iy, 0], [ix, iy - 1, 1], [ix, iy + 1, 1]]) {
      if (nx < 0 || ny < 0 || nx >= xs.length || ny >= ys.length) continue
      const nextId = ny * xs.length + nx, next = point(nextId), key = id < nextId ? `${id}:${nextId}` : `${nextId}:${id}`
      let clear = clearSegments.get(key)
      if (clear === undefined) { clear = !boxes.some(box => routeSegmentIntersectsBox(current, next, box, clearance)); clearSegments.set(key, clear) }
      if (!clear) continue
      const state = nextId * 2 + direction, cost = entry.cost + distance(current, next) + (axis === direction ? 0 : bendPenalty)
      if (cost >= (costs.get(state) ?? Infinity)) continue
      costs.set(state, cost); previous.set(state, entry.state); origins.set(state, origins.get(entry.state)!); queue.push({ state, cost, estimate: cost + heuristic(next) })
    }
  }
  if (!best) return null
  const start = origins.get(best.state)!, points: RoutingPoint[] = []
  for (let state: number | undefined = best.state; state !== undefined; state = previous.get(state)) points.unshift(point(Math.floor(state / 2)))
  points.unshift(start.anchor); points.push(best.goal.anchor)
  const simplified = points.filter((p, i) => !i || i === points.length - 1 || !((points[i - 1].x === p.x && p.x === points[i + 1].x) || (points[i - 1].y === p.y && p.y === points[i + 1].y)))
  const lengths = simplified.slice(1).map((p, i) => distance(p, simplified[i]))
  let half = lengths.reduce((sum, length) => sum + length, 0) / 2, label = simplified[0]
  for (let i = 0; i < lengths.length; i++) {
    if (half <= lengths[i]) { const a = simplified[i], b = simplified[i + 1], ratio = half / lengths[i]; label = { x: a.x + (b.x - a.x) * ratio, y: a.y + (b.y - a.y) * ratio }; break }
    half -= lengths[i]
  }
  return { sourceHandle: `source-${start.side}`, targetHandle: `target-${best.goal.side}`, points: simplified, path: roundedPath(simplified), labelX: label.x, labelY: label.y }
}

let lastRoutes: { key: string; routes: Map<string, RoutedConnection | null> } | undefined
/** Geometry caching keeps zooming and live status updates out of the pathfinder. */
export function routeTopologyDependencies(boxes: RoutingBox[], connections: { id: string; from: string; to: string }[]) {
  const key = JSON.stringify([boxes, connections])
  if (lastRoutes?.key === key) return lastRoutes.routes
  const routes = new Map(connections.map(edge => [edge.id, routeTopologyConnection(boxes, edge.from, edge.to)]))
  lastRoutes = { key, routes }
  return routes
}
