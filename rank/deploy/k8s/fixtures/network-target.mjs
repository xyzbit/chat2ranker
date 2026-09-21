/** Listening TCP/UDP targets record probe arrivals without external traffic. */
import { createServer } from 'node:http';
import { createSocket } from 'node:dgram';
const arrivals = [];
for (const port of [8800, 8801]) {
  createServer((request, response) => {
    if (request.url === '/facts') { response.end(JSON.stringify(arrivals)); return; }
    arrivals.push({ protocol: 'tcp', port, path: request.url });
    response.end('reachable');
  }).listen(port, '0.0.0.0');
}
const socket = createSocket('udp4');
socket.on('message', (message, peer) => {
  arrivals.push({ protocol: 'udp', port: 8802, path: message.toString() });
  socket.send(message, peer.port, peer.address);
});
socket.bind(8802, '0.0.0.0');
