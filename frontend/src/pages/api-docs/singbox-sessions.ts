import type { Section } from './endpoints.ts';

export const singBoxSessionSections: readonly Section[] = [
  {
    id: 'singbox-sessions',
    title: 'Sing-box sessions',
    description:
      'Inspect active sing-box connections and revoke sessions without replacing the existing traffic collector. Omit nodeId (or use 0) for the local panel; provide a positive nodeId to dispatch through the multi-node runtime.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/server/singbox/sessions',
        summary:
          'List active sing-box sessions with inbound, user, outbound, destination, and traffic metadata.',
        params: [
          {
            name: 'nodeId',
            in: 'query',
            type: 'number',
            desc: 'Optional remote node ID. Omit or use 0 for the local panel.',
            optional: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/server/singbox/sessions/disconnect-user',
        summary:
          'Disconnect active sing-box sessions for a user, optionally scoped to one inbound and/or remote node.',
        params: [
          {
            name: 'nodeId',
            in: 'body',
            type: 'number',
            desc: 'Optional remote node ID. Omit or use 0 for the local panel.',
            optional: true,
          },
          {
            name: 'inbound',
            in: 'body',
            type: 'string',
            desc: 'Optional inbound tag used to scope the disconnect.',
            optional: true,
          },
          {
            name: 'user',
            in: 'body',
            type: 'string',
            desc: 'Authenticated sing-box user identifier.',
          },
        ],
        body: '{\n  "nodeId": 2,\n  "inbound": "in-443-tcp",\n  "user": "alice@example.com"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/singbox/sessions/disconnect-users',
        summary:
          'Disconnect active sing-box sessions for several users using one connection snapshot, optionally scoped to an inbound and/or remote node.',
        params: [
          {
            name: 'nodeId',
            in: 'body',
            type: 'number',
            desc: 'Optional remote node ID. Omit or use 0 for the local panel.',
            optional: true,
          },
          {
            name: 'inbound',
            in: 'body',
            type: 'string',
            desc: 'Optional inbound tag used to scope the disconnect.',
            optional: true,
          },
          {
            name: 'users',
            in: 'body',
            type: 'string[]',
            desc: 'Authenticated sing-box user identifiers to revoke.',
          },
        ],
        body: '{\n  "nodeId": 2,\n  "users": ["alice@example.com", "bob@example.com"]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/singbox/sessions/disconnect-inbound',
        summary: 'Disconnect all active sing-box sessions for an inbound on the local panel or a selected remote node.',
        params: [
          {
            name: 'nodeId',
            in: 'body',
            type: 'number',
            desc: 'Optional remote node ID. Omit or use 0 for the local panel.',
            optional: true,
          },
          {
            name: 'inbound',
            in: 'body',
            type: 'string',
            desc: 'Inbound tag whose active sing-box sessions should be closed.',
          },
        ],
        body: '{\n  "nodeId": 2,\n  "inbound": "in-443-tcp"\n}',
      },
    ],
  },
];
