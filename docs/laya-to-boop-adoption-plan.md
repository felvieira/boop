# Plano para trazer ideias do Laya ao Boop

Data: 2026-10-16
Base local: `main` em `64cd0b8` (`feat: add support kanban and hourly triage`).

## Recomendação

Adotar os conceitos de operação e aprendizado do Laya em etapas, mantendo a arquitetura do Boop: servidor Go, SQLite, web/PWA e notificações iOS. O Boop já coleta notificações dos projetos conectados e já tem o quadro e o classificador. Não precisa virar um agregador de Gmail/Slack nem incorporar o desktop Tauri, o n8n, Python ou ChromaDB do Laya.

Ordem recomendada:

1. Tornar a triagem auditável e recuperável pela interface.
2. Registrar correções humanas e criar regras revisáveis para melhorar a classificação.
3. Adicionar busca, relação entre cartões e métricas de qualidade/custo.
4. Gerar um resumo diário útil do quadro.
5. Só depois avaliar ações externas aprovadas pelo usuário.

## Estado da implementação

As fases 1 a 4 foram implementadas no checkout local. A fase 5 também está implementada como preparação e execução revisada: cada cartão pode guardar rascunho de issue, resposta ou plano técnico; issues usam um destino GitHub mapeado pelo produto e só são criadas após confirmação explícita. O token GitHub ainda precisa ser configurado em Settings e a migração `0011_support_actions.sql` precisa ser aplicada quando a nova versão iniciar.

Respostas são copiadas ou abertas no cliente de e-mail como rascunho; o Boop não as envia. Planos aprovados viram itens do checklist do cartão. A criação de issue é idempotente pelo marcador do cartão, registra aprovador, hash do conteúdo, resultado e identificador externo, e pede reconciliação quando a resposta do GitHub é incerta. O código local foi compilado, mas nenhuma ação real foi executada no GitHub nem houve deploy.

## O que o Boop já tem

- Quadro com listas configuráveis, arrastar e soltar, cartões manuais e de IA, busca, filtro por projeto, prioridade, etiquetas, vencimento, checklist, comentários, anexos, arquivo e restauração.
- Worker que processa notificações pendentes na inicialização e depois de hora em hora; filas persistentes, leases, tentativas com backoff e upsert idempotente por evento.
- Estado por notificação com categoria, título, digest, quantidade de tentativas e último erro.
- Chave e modelo globais do OpenRouter configuráveis na interface, com chave criptografada no servidor.
- Remoção de padrões de e-mail/telefone em texto e de campos estruturados comuns de identificação/credenciais.

O contexto enviado ao modelo remove e-mails, telefones, campos de identificação e nomes reconhecidos em dados estruturados, além de nomes apresentados por frases comuns como “meu nome é” ou “me chamo”. Essa detecção local não reconhece com garantia nomes soltos em qualquer texto livre; o Boop não envia o texto original a outro modelo para tentar detectá-los. A interface original mostrava somente totais, sem uma tela por evento, motivo, erro ou retry. A classificação também não era um campo estruturado e editável do cartão.

Referências locais: `docs/support-board.md`, `server/internal/triage/triage.go`, `server/migrations/0005_notification_triage.sql`, `server/internal/api/api.go` e `server/web/src/pages/SupportBoard.svelte`.

## Plano por fases

### Fase 1 — auditoria e recuperação da triagem

Prioridade: máxima. Esforço: médio.

- Criar uma tela de atividade da IA acessível a partir de Support, com uma linha por notificação e estados claros: aguardando, processando, cartão criado, filtrado e falha.
- Corrigir e validar a sanitização do contexto antes de ampliar o que é enviado ao modelo; cobrir nomes em texto livre, contatos e credenciais sem registrar o prompt bruto nos logs.
- Mostrar projeto, origem, data, decisão/categoria, resumo, motivo de filtragem, tentativa atual, último erro e próxima tentativa.
- Permitir abrir a notificação/cartão, repetir uma falha e reenfileirar uma notificação ignorada com ação explícita.
- Exibir última execução, próxima execução e saúde/configuração do worker.
- Registrar eventos de auditoria para mudanças humanas importantes: mover, editar classificação/prioridade, arquivar, restaurar e repetir triagem.
- Preservar o contrato já definido: notificações concluídas não voltam ao modelo por causa do ciclo horário; repetição exige ação explícita; qualquer repetição continua idempotente e não cria cartão duplicado.

Critérios de aceite: consigo localizar uma notificação filtrada e entender por quê; consigo identificar e repetir uma falha; a repetição não duplica cartão nem altera sua lista manualmente escolhida; cartões já analisados não são processados novamente pelo ciclo normal.

### Fase 2 — classificação estruturada, regras e aprendizado supervisionado

Prioridade: alta. Esforço: médio para classificação e feedback; médio/alto para o editor de regras.

- Guardar no cartão campos estruturados para categoria (bug, correção, sugestão, parceria/afiliado, solicitação de cliente, problema interno e outros), prioridade sugerida e estado de revisão. Manter categoria separada da lista/status do Kanban.
- Ter uma fila ou filtro para baixa confiança e dados incompletos, em vez de esconder casos ambíguos entre os filtrados.
- Registrar o valor previsto pela IA e a correção feita pelo usuário. Mudanças de categoria, prioridade, inclusão/filtragem e vínculo de duplicidade viram feedback utilizável.
- Começar com regras determinísticas editáveis: incluir, filtrar, etiquetar, sugerir prioridade ou encaminhar para revisão. Regras podem ser globais ou limitadas a um projeto, mantendo uma única configuração global de provedor/modelo.
- Oferecer modo de simulação da regra sobre notificações anteriores antes de ativá-la. Cada aplicação registra a regra, o resultado e o motivo; nenhuma regra nova descarta silenciosamente eventos.
- Fazer a IA sugerir regras a partir de correções recorrentes, mas exigir aprovação humana antes de ativá-las. Mostrar exemplos que justificam a sugestão e permitir editar, pausar ou remover.
- Tornar as instruções globais da triagem configuráveis em Settings, com padrão restaurável, versão usada por cada decisão e prévia/teste em eventos sintéticos.

Critérios de aceite: a categoria e prioridade podem ser filtradas sem depender do texto do título; uma correção fica registrada; uma regra pode ser simulada, ativada e desativada; sugestões aprendidas nunca passam a valer sozinhas.

### Fase 3 — busca, contexto, duplicatas e métricas

Prioridade: média/alta. Esforço: médio.

- Implementar busca no servidor por cartões e eventos antigos, além da busca atual no conjunto carregado no navegador. Adicionar filtros combináveis por projeto, categoria, prioridade, lista e período.
- Encontrar relações primeiro por identificadores e URLs exatos (issue, pedido, código de erro). Depois, oferecer candidatos semelhantes por título/resumo. Sempre pedir confirmação antes de unir ou marcar como duplicata.
- Mostrar cartões relacionados e histórico da mesma pessoa/entidade somente quando a ligação puder ser feita com segurança; não expor e-mail/telefone como chave de busca pública da interface.
- Medir volume processado, cartões criados, filtrados por motivo, falhas/retries, tempo de processamento e correções humanas, por período, projeto e categoria.
- Capturar tokens/uso devolvidos pelo provedor quando disponíveis e calcular custo estimado por modelo. Identificar a estimativa como tal e não armazenar a chave nem prompts integrais no relatório.
- Permitir exportação administrativa CSV/JSON de cartões e auditoria com filtros por período, excluindo credenciais.
- Adicionar fixar/desafixar cartão como conveniência opcional; prioridade, etiquetas e listas já cobrem a maior parte desse uso.

Critérios de aceite: a busca encontra registros antigos sem carregar o quadro inteiro; cartões parecidos são apenas sugeridos; relatórios distinguem uso medido de custo estimado; exportação é protegida pela autenticação administrativa.

### Fase 4 — resumo diário do trabalho

Prioridade: média. Esforço: pequeno/médio.

- Criar um resumo configurável com cartões novos, urgentes, bloqueados, vencidos/antigos e leads de parceria, agrupados por projeto e categoria.
- Calcular o resumo a partir dos cartões existentes sempre que possível; usar o modelo apenas para sintetizar linguagem quando houver conteúdo novo relevante.
- Mostrar o resumo em Support e, se habilitado, enviá-lo pelo canal de push já usado pelo Boop. Definir horário e opção de desligar; sem e-mail automático.
- Guardar a data do último resumo para evitar repetição de conteúdo e custo quando nada mudou.

Critérios de aceite: o resumo informa o que mudou desde o anterior, destaca itens que exigem atenção e não chama o modelo quando não há novidades relevantes.

### Fase 5 — ações externas com aprovação

Prioridade: concluída no código local. Ativação do GitHub depende da configuração do token e do mapa de repositórios.

- Preparar por cartão uma issue GitHub, uma resposta ao solicitante ou um plano técnico convertido em checklist após aprovação.
- Manter o token GitHub criptografado em Settings e mapear o nome do produto para `owner/repository`; o destino continua editável e visível na revisão.
- Exibir destino e conteúdo integral antes de cada criação de issue ou aplicação de plano; a confirmação explícita é registrada junto do usuário administrador.
- Guardar hash do payload, resumo, aprovador, resultado, erro e identificador externo. A issue leva uma chave idempotente; estados incertos exigem reconciliação e confirmação antes de uma nova tentativa.
- Respostas são apenas copiadas ou abertas como rascunho em um cliente de e-mail. Boop não envia mensagens. Criação de PR e agente de pesquisa/correção seguem adiados até haver integração e ambiente isolado; o servidor não presume acesso aos repositórios locais.

Critérios de aceite: atendidos pelo fluxo local de revisão; validar a criação real de issue depois de configurar um token com acesso Issues de leitura e escrita no repositório desejado.

## Ideias do Laya que não devem entrar agora

| Ideia do Laya | Decisão para o Boop |
| --- | --- |
| Conectores diretos para Gmail, Slack, Jira, GitHub, Notion e calendários | Não duplicar a ingestão atual. As notificações já chegam ao Boop por projetos e integrações. Criar um conector específico só para uma fonte que realmente não consiga publicar eventos no Boop. |
| Spaces por fonte e por chave/modelo | Usar os projetos existentes como agrupamento. Manter a chave e o modelo globais, conforme a decisão já tomada para o Boop. |
| Tauri + Svelte + Python + n8n + ChromaDB | Não portar a arquitetura. Ela resolve a execução local e a integração de muitos serviços; o Boop é um servidor compacto com Go e SQLite. |
| Múltiplas personas de IA | Não necessário para o primeiro ciclo. Categorias explícitas e regras por projeto cobrem o fluxo de suporte com menos custo e configuração. |
| Backends de inferência por CLI de agentes | Não adequado ao worker hospedado no Coolify, que precisa de execução previsível sem sessão desktop. Manter OpenRouter global por enquanto. |
| Chat livre sobre todas as notificações | Adiar até auditoria e busca estarem prontas. Se entrar, restringir a perguntas sobre dados que o administrador pode ver e exibir as fontes usadas na resposta. |
| Execução automática de e-mails, mensagens, PRs e outros efeitos externos | Não ativar automaticamente. Se houver demanda, implementar a fase 5 com prévia e aprovação explícita. |
| Busca vetorial/Chroma desde o início | Evitar infraestrutura e custo adicionais. Começar com identificadores, URLs e busca textual; medir se a similaridade semântica é necessária depois. |

## Dependências e decisões de produto

- A triagem continua global para todos os projetos do Boop e usa a configuração global do OpenRouter já existente.
- O ciclo horário continua sendo o mecanismo de descoberta; o quadro atualiza os resultados pela API. A primeira execução processa pendências históricas, e o trabalho concluído não é repetido automaticamente.
- Os dados originais permanecem no armazenamento privado do Boop. O contexto enviado ao modelo deve remover PII/credenciais de forma verificável e mostrar claramente que o provedor escolhido recebe o texto sanitizado.
- Regras, feedback e auditoria precisam diferenciar decisão da IA, regra determinística e ação manual.
- Não importar código do Laya para esta evolução. Adaptar os conceitos; se futuramente algum código for copiado, respeitar Apache-2.0 e preservar `NOTICE` e avisos aplicáveis.

## Fontes consultadas

- [Laya README e funcionalidades](https://github.com/aayushch/laya)
- [Arquitetura e pipeline do Laya](https://github.com/aayushch/laya/blob/main/docs/architecture.md)
- [Contratos de API do Laya](https://github.com/aayushch/laya/blob/main/docs/api-contracts.md)
- [Licença e releases do Laya](https://github.com/aayushch/laya/releases)
