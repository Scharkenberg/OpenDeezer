/* Thin main() for the standalone purpledisc-gnome executable. The real entry is
 * purpledisc_run() in main.c (also exported from libpurpledisc-gtk.so for the
 * unified Linux launcher). */
int purpledisc_run(int argc, char **argv);

int main(int argc, char **argv) { return purpledisc_run(argc, argv); }
