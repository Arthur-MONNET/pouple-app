<script setup lang="ts">
import done from '../assets/mascot/mascot-done.svg';
import investigate from '../assets/mascot/mascot-investigate.svg';
import multitask from '../assets/mascot/mascot-multitask.svg';
import oops from '../assets/mascot/mascot-oops.svg';
import sleep from '../assets/mascot/mascot-sleep.svg';
import stop from '../assets/mascot/mascot-stop.svg';

export type MascotPose = 'multitask' | 'investigate' | 'done' | 'sleep' | 'stop' | 'oops';

/** One drawing, with its navy outline, on every surface: the octopus keeps the same look wherever it appears. */
const SOURCES: Record<MascotPose, string> = { multitask, investigate, done, sleep, stop, oops };

/** The octopus is decoration: it never carries information, so it has no alternative text. */
defineProps<{ pose: MascotPose }>();
</script>

<template>
  <img :src="SOURCES[pose]" alt="" draggable="false" class="select-none object-contain" />
</template>
