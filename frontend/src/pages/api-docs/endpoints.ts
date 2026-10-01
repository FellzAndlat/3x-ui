export type {
  HttpMethod,
  ParamLocation,
  ParamType,
  EndpointParam,
  Endpoint,
  SubscriptionHeader,
  Section,
} from './endpoints.base.ts';

import { sections as baseSections } from './endpoints.base.ts';
import type { Section } from './endpoints.base.ts';

const coreDiagnostics: Section = {
  id: 'core-diagnostics',
  title: 'Core Capabilities and Diagnostics',
  description:
    'Inspect panel-supported capabilities for the selected proxy core and run core-specific diagnostics.',
  endpoints: [
    {
      method: 'GET',
      path: '/panel/api/server/core/capabilities',
      summary: 'Return the selected core and the optional capabilities implemented by 3X-UI.',
      response:
        '{\n  "success": true,\n  "obj": {\n    "core": "sing-box",\n    "capabilities": {\n      "ruleSets": true,\n      "outboundDelayProbe": true,\n      "connectionStats": true,\n      "hotInboundReload": false,\n      "clashApi": true,\n      "geoIp": false,\n      "geoSite": false,\n      "shadowTls": true\n    }\n  }\n}',
    },
    {
      method: 'POST',
      path: '/panel/api/server/singbox/outbound/check',
      summary: 'Test a running sing-box outbound by tag and return its measured delay.',
      params: [
        {
          name: 'tag',
          in: 'body (form)',
          type: 'string',
          desc: 'Outbound tag to test (required).',
        },
        {
          name: 'url',
          in: 'body (form)',
          type: 'string',
          desc: 'HTTP(S) URL used by sing-box for the probe.',
          optional: true,
          defaultValue: 'https://www.gstatic.com/generate_204',
        },
        {
          name: 'timeout',
          in: 'body (form)',
          type: 'integer',
          desc: 'Probe timeout in milliseconds. Allowed range: 1-30000.',
          optional: true,
          defaultValue: 5000,
        },
      ],
      response:
        '{\n  "success": true,\n  "obj": {\n    "tag": "proxy",\n    "url": "https://www.gstatic.com/generate_204",\n    "delay": 82,\n    "delay2": 0,\n    "timeout": 5000\n  }\n}',
      errorResponse:
        '{\n  "success": false,\n  "msg": "selected core does not support outbound delay probes | sing-box is not running | outbound tag is required | timeout must be between 1 and 30000 milliseconds | <Clash API error>"\n}',
    },
  ],
};

export const sections: readonly Section[] = [...baseSections, coreDiagnostics];
