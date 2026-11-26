// Thin main() for the standalone purpledisc-kde executable. The real entry is
// purpledisc_run() in main.cpp (also exported from libpurpledisc-qt.so for the
// unified Linux launcher).
extern "C" int purpledisc_run(int argc, char **argv);

int main(int argc, char **argv) { return purpledisc_run(argc, argv); }
