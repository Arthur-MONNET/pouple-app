import type { Lang } from '../i18n';

/**
 * Every sentence the mock backend writes, in both languages: like the real core, it speaks the interface language,
 * so the whole web app can be reviewed in French and in English without the core.
 */
export interface MockText {
  slack: {
    tagline: string;
    label: string;
    help: string;
    patternHint: string;
    guide: string[];
    linkLabel: string;
    live: { label: string; help: string; hint: string; title: string; body: string };
  };
  codex: { tagline: string; guide: string[]; install: string; login: string };
  profiles: {
    slack: { subtitle: string; facts: [string, string, string]; values: [string, string, string]; warningTitle: string; warningDetail: string };
    codex: { subtitle: string; facts: [string, string, string, string]; until: string };
  };
  errors: {
    required: string;
    botToken: string;
    invalidAuth: string;
    notInstalled: string;
    notSignedIn: string;
    notFolder: (path: string) => string;
    negativeLimit: string;
  };
  logs: { install: [string, string]; login1: [string, string]; login2: string };
  paused: string;
  replies: { answered: string; applied: string; more: (asked: string) => string; again: string; filesOnly: string };
  fixtures: {
    /** The Slack messages, in the order of the fixtures. */
    q: string[];
    steps: [string, string];
    stagingEntry: string;
    stagingSummary: string;
    staging: { q1: string; prod: string; prodDesc: string; preview: string; q2: string; yes: string; yesDesc: string; no: string };
    footer: { title: string; description: string; entry: string; summary: string; approve: string; decline: string };
    login: string;
    loginSummary: string;
    loginDraft: string;
    rename: string;
    noteDone: string;
    limit: string;
  };
}

const LOGIN_FR = `Le formulaire de connexion poste sur \`/api/session\`, mais la requête part avant que le cookie CSRF soit posé.

**Ce que j’ai trouvé**
- \`LoginForm.vue\` appelle \`submit()\` au montage quand le navigateur remplit les champs automatiquement.
- Le cookie CSRF est créé par \`useSession()\`, qui se résout après le premier rendu.
- Sur une connexion lente, le premier POST part donc sans cookie et l’API répond 403.

**Pourquoi c’est intermittent**
Ça n’arrive que si le remplissage automatique se déclenche avant la fin de l’initialisation de la session : Safari le fait presque toujours, Chrome rarement.

1. Attendre \`useSession().ready\` avant le premier envoi.
2. Désactiver le bouton tant que le cookie n’existe pas.
3. Ajouter un test de non-régression avec une initialisation retardée.

\`\`\`ts
await session.ready;
await api.post('/session', credentials);
\`\`\`

Je n’ai rien modifié. Dis-moi et je prépare le correctif dans le projet \`front\`.`;

const LOGIN_EN = `The login form posts to \`/api/session\`, but the request is sent before the CSRF cookie is set.

**What I found**
- \`LoginForm.vue\` calls \`submit()\` on mount when the browser autofills the fields.
- The CSRF cookie is created by \`useSession()\`, which resolves after the first paint.
- On a slow connection the first POST therefore goes out without the cookie and the API answers 403.

**Why it is intermittent**
It only happens when the autofill fires before the session bootstrap ends: Safari does it most of the time, Chrome rarely.

1. Await \`useSession().ready\` before the first submit.
2. Disable the button until the cookie exists.
3. Add a regression test with a delayed bootstrap.

\`\`\`ts
await session.ready;
await api.post('/session', credentials);
\`\`\`

I did not change anything. Say the word and I will prepare the fix in the \`front\` project.`;

const fr: MockText = {
  slack: {
    tagline: 'Là où arrivent tes demandes',
    label: 'Jeton OAuth utilisateur',
    help: 'Il commence par xoxp-. Il reste dans le trousseau de ton Mac et ne s’affiche plus jamais.',
    patternHint: 'ne ressemble pas à un jeton Slack (il commence par xoxp-)',
    guide: [
      'Crée l’app Slack',
      'Un clic : Slack s’ouvre avec le nom et toutes les permissions déjà remplis. Choisis ton espace de travail et confirme.',
      'Installe-la dans ton espace de travail',
      'Sur la page de l’app, ouvre Install App, clique sur Install to Workspace, puis Allow.',
      'Copie le jeton OAuth utilisateur',
      'Il est sur la même page et commence par xoxp-. Colle-le ici.',
    ],
    linkLabel: 'Créer l’app Slack',
    live: {
      label: 'Jeton de niveau application',
      help: 'Facultatif. Pour recevoir les messages en direct, colle le jeton xapp- sans rotation fourni par Slack. Il reste dans le trousseau de ton Mac.',
      hint: 'doit être un jeton xapp- sans rotation. Colle-le tel que Slack l’a fourni ou laisse ce champ vide.',
      title: 'Active la réception en direct',
      body: 'Dans Basic Information, crée un App-Level Token avec connections:write. Pour une application Slack existante, active Socket Mode et abonne-toi à message.channels, message.groups, message.im et message.mpim dans Subscribe to events on behalf of users, puis réinstalle l’application.',
    },
  },
  codex: {
    tagline: 'L’assistant qui se penche sur tes demandes',
    guide: [
      'Installe Codex CLI',
      'Pouple utilise Codex d’OpenAI sur ton Mac. S’il n’est pas installé, un clic suffit.',
      'Connecte-toi avec ChatGPT',
      'Une page du navigateur s’ouvre : connecte-toi avec le compte que Pouple doit utiliser. Il a sa propre connexion et ne touche jamais à ton Codex habituel.',
      'Vérifie le compte',
      'Pouple affiche qui est connecté, pour que tu sois sûr de ton coup.',
    ],
    install: 'Installer Codex',
    login: 'Se connecter avec ChatGPT',
  },
  profiles: {
    slack: {
      subtitle: 'Espace Acme',
      facts: ['Espace de travail', 'E-mail', 'Fuseau horaire'],
      values: ['Acme (acme.slack.com)', 'arthur@acme.example', 'Europe/Paris'],
      warningTitle: 'Les réactions ne sont pas surveillées',
      warningDetail: 'La permission reactions:read manque : Pouple ne voit pas quand un message reçoit ton émoji. Tout le reste fonctionne.',
    },
    codex: { subtitle: 'Connecté avec ChatGPT', facts: ['Offre', 'Codex', 'Organisation', 'Abonnement jusqu’au'], until: '14 janv. 2027' },
  },
  errors: {
    required: 'Le jeton OAuth utilisateur est obligatoire',
    botToken: 'C’est un jeton de bot (xoxb-). Pouple a besoin du jeton OAuth utilisateur, qui commence par xoxp-.',
    invalidAuth: 'Slack refuse ce jeton (invalid_auth). Recopie-le depuis OAuth & Permissions.',
    notInstalled: 'Codex CLI n’est pas encore installé. Clique sur « Installer Codex ».',
    notSignedIn: 'Pas encore connecté. Clique sur « Se connecter avec ChatGPT ».',
    notFolder: (path) => `« ${path} » n’est pas un dossier`,
    negativeLimit: 'La limite quotidienne ne peut pas être négative (0 : pas de limite)',
  },
  logs: {
    install: ['Téléchargement de Codex depuis OpenAI…', 'Codex est installé. Tu peux maintenant te connecter.'],
    login1: ['Démarrage du serveur de connexion local sur http://localhost:1455.', 'Si ton navigateur ne s’est pas ouvert, va sur :'],
    login2: 'En attente de la fin de la connexion…',
  },
  paused: 'Codex a besoin d’une pause. On reprend à 14:30.',
  replies: {
    answered: 'Merci, ça règle la question. La variable existe en production mais n’a jamais été ajoutée aux secrets de staging : ajoute `STRIPE_WEBHOOK_SECRET` là-bas et redéploie.',
    applied: 'C’est fait : le pied de page utilise maintenant l’année en cours.',
    more: (asked) => `Un peu plus de détails.\n\n- J’ai relu les fichiers.\n- Rien d’autre n’en dépend.\n\nDemandé : ${asked}`,
    again: 'J’ai regardé de nouveau et la réponse est la même : rien n’a changé de mon côté.',
    filesOnly: '(fichiers seulement)',
  },
  fixtures: {
    q: [
      '@arthur tu sais pourquoi l’export des factures est vide pour mars ?',
      'Tu peux regarder le déploiement staging qui plante ? Je pense que c’est la nouvelle variable d’env.',
      'Le pied de page affiche encore 2023, tu peux corriger ?',
      'Le login est capricieux sur Safari, le formulaire répond parfois 403. Une idée ?',
      'Tu peux renommer le libellé « Workspace » en « Équipe » partout dans les réglages ?',
      'Note à moi-même : passer Node en version 22 dans l’image CI.',
      '@here la commande des repas ferme à midi !',
      'Tu peux regarder le pic de mémoire sur le worker ?',
      'Petite question : quel port utilise l’API en local ?',
    ],
    steps: ['Lecture de src/billing/export.ts', 'Lecture de LoginForm.vue'],
    stagingEntry: 'Les deux derniers déploiements ont échoué sur `STRIPE_WEBHOOK_SECRET`. Il me faut savoir avec quel environnement comparer.',
    stagingSummary: 'Deux déploiements staging ont échoué : il manque une variable.',
    staging: {
      q1: 'Avec quel environnement je compare ?',
      prod: 'Production',
      prodDesc: 'Vérifier ce que le déploiement en ligne utilise.',
      preview: 'Preview',
      q2: 'Je lis aussi les logs de la CI ?',
      yes: 'Oui, lis les logs',
      yesDesc: 'En lecture seule, les 200 dernières lignes.',
      no: 'Non, seulement la configuration',
    },
    footer: {
      title: 'Utiliser l’année en cours dans AppFooter.vue',
      description: 'Remplacer le 2023 écrit en dur par new Date().getFullYear().',
      entry: 'L’année est écrite en dur dans `AppFooter.vue`. Une ligne suffit pour la remplacer par l’année en cours.',
      summary: 'L’année est écrite en dur dans le pied de page.',
      approve: 'Oui, faire le changement',
      decline: 'Non, ne pas toucher au code',
    },
    login: LOGIN_FR,
    loginSummary: 'Le premier POST court contre le cookie CSRF.',
    loginDraft: 'Merci pour le signalement. C’est une course entre l’envoi automatique et le cookie CSRF : j’attends la session avant le premier envoi et je livre ça aujourd’hui.',
    rename: 'J’ai trouvé 6 occurrences dans `settings/`. J’applique le renommage.',
    noteDone: 'C’est fait : `.github/workflows/ci.yml` utilise maintenant Node 22. Le changement est commité sur une branche locale, rien n’est poussé.',
    limit: 'Codex a besoin d’une pause. On reprend à 18:30.',
  },
};

const en: MockText = {
  slack: {
    tagline: 'Where your requests come from',
    label: 'User OAuth Token',
    help: 'Starts with xoxp-. It stays in your Mac’s keychain and is never shown again.',
    patternHint: 'does not look like a Slack token (it starts with xoxp-)',
    guide: [
      'Create the Slack app',
      'One click: Slack opens with the name and every permission already filled in. Choose your workspace and confirm.',
      'Install it to your workspace',
      'On the app’s page, open Install App and click Install to Workspace, then Allow.',
      'Copy the User OAuth Token',
      'It is on the same page and starts with xoxp-. Paste it here.',
    ],
    linkLabel: 'Create the Slack app',
    live: {
      label: 'App-level Token',
      help: 'Optional. For live updates, paste the non-rotating xapp- token from Slack. It stays in your Mac’s keychain.',
      hint: 'must be a non-rotating xapp- token. Paste it as issued by Slack, or leave this field empty.',
      title: 'Enable live Slack updates',
      body: 'In Basic Information, generate an App-Level Token with connections:write. For an existing Slack app, enable Socket Mode and subscribe to message.channels, message.groups, message.im, and message.mpim under Subscribe to events on behalf of users, then reinstall the app.',
    },
  },
  codex: {
    tagline: 'The assistant that looks into your requests',
    guide: [
      'Install the Codex CLI',
      'Pouple uses OpenAI’s Codex on your Mac. If it is not installed, one click installs it.',
      'Sign in with ChatGPT',
      'A browser page opens: sign in with the account you want Pouple to use. It has its own login and never touches your usual Codex.',
      'Check the account',
      'Pouple shows who is signed in, so you can be sure it is the right one.',
    ],
    install: 'Install Codex',
    login: 'Sign in with ChatGPT',
  },
  profiles: {
    slack: {
      subtitle: 'Acme workspace',
      facts: ['Workspace', 'E-mail', 'Time zone'],
      values: ['Acme (acme.slack.com)', 'arthur@acme.example', 'Europe/Paris'],
      warningTitle: 'Reactions are not watched',
      warningDetail: 'The reactions:read permission is missing, so Pouple cannot see when a message gets your emoji. Everything else works.',
    },
    codex: { subtitle: 'Signed in with ChatGPT', facts: ['Plan', 'Codex', 'Organization', 'Subscription until'], until: '14 Jan 2027' },
  },
  errors: {
    required: 'User OAuth Token is required',
    botToken: 'This is a bot token (xoxb-). Pouple needs the User OAuth Token, which starts with xoxp-.',
    invalidAuth: 'Slack does not accept this token (invalid_auth). Copy it again from OAuth & Permissions.',
    notInstalled: 'The Codex CLI is not installed yet. Click “Install Codex”.',
    notSignedIn: 'Not signed in yet. Click “Sign in with ChatGPT”.',
    notFolder: (path) => `“${path}” is not a folder`,
    negativeLimit: 'The daily limit cannot be negative (0 means no limit)',
  },
  logs: {
    install: ['Downloading Codex from OpenAI…', 'Codex is installed. You can now sign in.'],
    login1: ['Starting local login server on http://localhost:1455.', 'If your browser did not open, visit:'],
    login2: 'Waiting for the sign-in to finish…',
  },
  paused: 'Codex needs a breather. Back at 14:30.',
  replies: {
    answered: 'Thanks, that settles it. The variable exists in production but was never added to the staging secrets: add `STRIPE_WEBHOOK_SECRET` there and redeploy.',
    applied: 'Done: the footer now uses the current year.',
    more: (asked) => `Here is a bit more detail on that.\n\n- I looked at the files again.\n- Nothing else depends on it.\n\nAsked: ${asked}`,
    again: 'I looked again and the answer is the same: nothing changed on my side.',
    filesOnly: '(files only)',
  },
  fixtures: {
    q: [
      '@arthur do you know why the invoice export is empty for the March period?',
      'Can you check the failing deploy on staging? I think it is the new env var.',
      'The footer still shows 2023, can you fix it?',
      'Login is flaky on Safari, the form sometimes answers 403. Any idea?',
      'Please rename the "Workspace" label to "Team" everywhere in settings.',
      'Note to self: bump the Node version in the CI image to 22.',
      '@here lunch order closes at noon!',
      'Could you look at the memory spike on the worker?',
      'Quick one: which port does the local API use?',
    ],
    steps: ['Reading src/billing/export.ts', 'Reading LoginForm.vue'],
    stagingEntry: 'The last two deploys failed on `STRIPE_WEBHOOK_SECRET`. I need to know which environment to look at.',
    stagingSummary: 'Two staging deploys failed with a missing variable.',
    staging: {
      q1: 'Which environment should I compare it with?',
      prod: 'Production',
      prodDesc: 'Check what the live deploy uses.',
      preview: 'Preview',
      q2: 'Should I look at the CI logs too?',
      yes: 'Yes, read the logs',
      yesDesc: 'Read-only, last 200 lines.',
      no: 'No, just the configuration',
    },
    footer: {
      title: 'Use the current year in AppFooter.vue',
      description: 'Replace the literal 2023 with new Date().getFullYear().',
      entry: 'The year is hardcoded in `AppFooter.vue`. A one-line change replaces it with the current year.',
      summary: 'The year is hardcoded in the footer.',
      approve: 'Yes, make the change',
      decline: 'No, leave the code alone',
    },
    login: LOGIN_EN,
    loginSummary: 'The first POST races the CSRF cookie.',
    loginDraft: 'Thanks for the report. It is a race between the autofill submit and the CSRF cookie: I will await the session before the first submit and ship it today.',
    rename: 'Found 6 occurrences in `settings/`. Applying the rename.',
    noteDone: 'Done: `.github/workflows/ci.yml` now uses Node 22. The change is committed on a local branch, nothing is pushed.',
    limit: 'Codex needs a breather. Back at 18:30.',
  },
};

export const mockText = (lang: Lang): MockText => (lang === 'fr' ? fr : en);
