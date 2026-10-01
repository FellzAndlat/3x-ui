import type { Section } from './endpoints.ts';

export const singBoxSessionSections: readonly Section[] = [
  {
    id: 'singbox-sessions',
    title: 'Sing-box sessions',
    description:
      'Inspect active sing-box connections and revoke sessions without replacing the existing traffic collector.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/server/singbox/sessions',
        summary:
          'List active sing-box sessions with inbound, user, outbound, destination, and traffic metadata.',
      },
      {
        method: 'POST',
        path: '/panel/api/server/singbox/sessions/disconnect-user',
        summary:
          'Disconnect active sing-box sessions for a user, optionally scoped to one inbound.',
        params: [
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
        body: '{\n  "inbound": "in-443-tcp",\n  "user": "alice@example.com"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/singbox/sessions/disconnect-inbound',
        summary: 'Disconnect all active sing-box sessions for an inbound.',
        params: [
          {
            name: 'inbound',
            in: 'body',
            type: 'string',
            desc: 'Inbound tag whose active sing-box sessions should be closed.',
          },
        ],
        body: '{\n  "inbound": "in-443-tcp"\n}',
      },
    ],
  },
];
